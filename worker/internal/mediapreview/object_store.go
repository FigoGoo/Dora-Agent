package mediapreview

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

const (
	maxObjectKeyBytes     = 1024
	maxObjectComponentLen = 200
)

// objectStore 持有一个 0700 本地对象根及其目录句柄，所有文件操作都限制在该根内。
type objectStore struct {
	// rootPath 是只供 Worker 内部命令使用且不会进入回执的规范绝对根路径。
	rootPath string
	// root 是限制所有普通文件操作范围的目录句柄。
	root *os.Root
}

// newObjectStore 校验绝对根目录、inode 类型和 0700 权限后固定目录句柄。
//
// 根目录不存在、是符号链接、权限过宽或在校验与打开之间被替换时失败关闭。
func newObjectStore(rootPath string) (*objectStore, error) {
	if rootPath == "" || strings.IndexByte(rootPath, 0) >= 0 || !filepath.IsAbs(rootPath) {
		return nil, newArtifactError(ErrorCodeInvalidArgument, "validate_object_root", nil)
	}
	cleanRoot := filepath.Clean(rootPath)
	rootInfo, err := os.Lstat(cleanRoot)
	if err != nil {
		return nil, newArtifactError(ErrorCodeInvalidArgument, "validate_object_root", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() || rootInfo.Mode().Perm() != 0o700 {
		return nil, newArtifactError(ErrorCodeInvalidArgument, "validate_object_root", nil)
	}

	root, err := os.OpenRoot(cleanRoot)
	if err != nil {
		return nil, newArtifactError(ErrorCodeInvalidArgument, "open_object_root", err)
	}
	openedInfo, err := root.Stat(".")
	if err != nil || !openedInfo.IsDir() || !os.SameFile(rootInfo, openedInfo) {
		_ = root.Close()
		return nil, newArtifactError(ErrorCodeInvalidArgument, "open_object_root", err)
	}
	return &objectStore{rootPath: cleanRoot, root: root}, nil
}

// Close 关闭对象根目录句柄；Engine 生命周期结束后不得继续执行产物操作。
func (s *objectStore) Close() error {
	if s == nil || s.root == nil {
		return nil
	}
	return s.root.Close()
}

// absolutePath 把已校验的 Business 相对 key 转换为仅供 Worker 内部命令使用的绝对路径。
//
// 转换后再次使用 filepath.Rel 复核路径仍位于对象根内，绝对路径不会进入回执或脱敏错误文本。
func (s *objectStore) absolutePath(key string) (string, error) {
	if err := validateObjectKey(key); err != nil {
		return "", err
	}
	resolved := filepath.Join(s.rootPath, filepath.FromSlash(key))
	relative, err := filepath.Rel(s.rootPath, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", newArtifactError(ErrorCodeInvalidArgument, "resolve_object_key", err)
	}
	return resolved, nil
}

// ensureSafeParents 校验 key 的所有父目录都已由 Business 创建，且不是符号链接或宽权限目录。
//
// Worker 不自动创建任意目录，从而避免 Job Payload 扩大本地文件系统写入范围。
func (s *objectStore) ensureSafeParents(key string) error {
	components := strings.Split(key, "/")
	current := ""
	for index := 0; index < len(components)-1; index++ {
		if current == "" {
			current = components[index]
		} else {
			current += "/" + components[index]
		}
		info, err := s.root.Lstat(current)
		if err != nil {
			return newArtifactError(ErrorCodeInvalidArgument, "validate_object_parent", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
			return newArtifactError(ErrorCodeInvalidArgument, "validate_object_parent", nil)
		}
	}
	return nil
}

// inspectOutput 校验输出父目录及已存在目标；已存在目标只能是 0600、单链接普通文件。
func (s *objectStore) inspectOutput(key string) error {
	if err := validateObjectKey(key); err != nil {
		return err
	}
	if err := s.ensureSafeParents(key); err != nil {
		return err
	}
	info, err := s.root.Lstat(key)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return newArtifactError(ErrorCodeInternal, "inspect_output", err)
	}
	if !isSafeRegularFile(info) {
		return newArtifactError(ErrorCodeInvalidArgument, "inspect_output", nil)
	}
	return nil
}

// createPart 以 O_EXCL 和 0600 创建当前 Attempt 独占的临时普通文件。
//
// 已存在 part 不会被删除或覆盖，因为它可能属于仍在运行或等待核对的另一执行。
func (s *objectStore) createPart(key string) (*os.File, error) {
	if err := validateObjectKey(key); err != nil {
		return nil, err
	}
	if err := s.ensureSafeParents(key); err != nil {
		return nil, err
	}
	file, err := s.root.OpenFile(key, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return nil, newArtifactError(ErrorCodeInternal, "create_artifact_part", err)
	}
	info, statErr := file.Stat()
	linkInfo, lstatErr := s.root.Lstat(key)
	if statErr != nil || lstatErr != nil || !isSafeRegularFile(info) || !os.SameFile(info, linkInfo) {
		_ = file.Close()
		_ = s.root.Remove(key)
		return nil, newArtifactError(ErrorCodeInternal, "create_artifact_part", errors.Join(statErr, lstatErr))
	}
	return file, nil
}

// openRegular 打开并复核一个 0600、单链接普通文件，拒绝符号链接和 inode 替换竞争。
func (s *objectStore) openRegular(key string) (*os.File, error) {
	return s.openRegularWithFlags(key, os.O_RDONLY)
}

// openRegularReadWrite 以读写方式打开并复核一个安全普通文件，供外部命令完成后的 Sync 使用。
func (s *objectStore) openRegularReadWrite(key string) (*os.File, error) {
	return s.openRegularWithFlags(key, os.O_RDWR)
}

// openRegularWithFlags 在固定目录句柄下打开文件，并以 Lstat/Fstat inode 一致性阻止替换竞争。
func (s *objectStore) openRegularWithFlags(key string, flags int) (*os.File, error) {
	if err := validateObjectKey(key); err != nil {
		return nil, err
	}
	if err := s.ensureSafeParents(key); err != nil {
		return nil, err
	}
	linkInfo, err := s.root.Lstat(key)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, newArtifactError(ErrorCodeNotFound, "open_artifact", err)
	}
	if err != nil {
		return nil, newArtifactError(ErrorCodeInternal, "open_artifact", err)
	}
	if !isSafeRegularFile(linkInfo) {
		return nil, newArtifactError(ErrorCodeArtifactInvalid, "open_artifact", nil)
	}

	file, err := s.root.OpenFile(key, flags, 0)
	if err != nil {
		return nil, newArtifactError(ErrorCodeInternal, "open_artifact", err)
	}
	openedInfo, err := file.Stat()
	if err != nil || !isSafeRegularFile(openedInfo) || !os.SameFile(linkInfo, openedInfo) {
		_ = file.Close()
		return nil, newArtifactError(ErrorCodeArtifactInvalid, "open_artifact", err)
	}
	return file, nil
}

// publishPart 将已验证 part 原子替换为 Business 指定的 staging key，并同步父目录元数据。
//
// rename 成功而目录 Sync 失败属于 Unknown Outcome，调用方必须按同一产物键核对，不能创建新键重试。
func (s *objectStore) publishPart(partKey string, targetKey string) error {
	partInfo, err := s.root.Lstat(partKey)
	if err != nil || !isSafeRegularFile(partInfo) {
		return newArtifactError(ErrorCodeArtifactInvalid, "publish_artifact", err)
	}
	if err := s.inspectOutput(targetKey); err != nil {
		return err
	}
	if err := s.root.Rename(partKey, targetKey); err != nil {
		return newArtifactError(ErrorCodeInternal, "publish_artifact", err)
	}

	parentKey := path.Dir(targetKey)
	directory, err := s.root.Open(parentKey)
	if err != nil {
		return newArtifactError(ErrorCodeUnknownOutcome, "sync_artifact_parent", err)
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return newArtifactError(ErrorCodeUnknownOutcome, "sync_artifact_parent", err)
	}
	if err := directory.Close(); err != nil {
		return newArtifactError(ErrorCodeUnknownOutcome, "sync_artifact_parent", err)
	}
	return nil
}

// removePart 只清理由当前调用成功创建且尚未 publish 的临时文件。
func (s *objectStore) removePart(key string) {
	if s == nil || s.root == nil {
		return
	}
	_ = s.root.Remove(key)
}

// validateObjectKey 校验 Business Object Key 是规范的 UTF-8 相对路径。
//
// 空值、绝对路径、点段、空段、反斜杠、NUL 和过长组件全部失败关闭。
func validateObjectKey(key string) error {
	if key == "" || len(key) > maxObjectKeyBytes || !utf8.ValidString(key) ||
		strings.IndexByte(key, 0) >= 0 || strings.Contains(key, `\`) ||
		strings.IndexFunc(key, unicode.IsControl) >= 0 || strings.HasPrefix(key, "/") ||
		filepath.IsAbs(key) || path.IsAbs(key) {
		return newArtifactError(ErrorCodeInvalidArgument, "validate_object_key", nil)
	}
	components := strings.Split(key, "/")
	for _, component := range components {
		if component == "" || component == "." || component == ".." || len(component) > maxObjectComponentLen {
			return newArtifactError(ErrorCodeInvalidArgument, "validate_object_key", nil)
		}
	}
	if path.Clean(key) != key {
		return newArtifactError(ErrorCodeInvalidArgument, "validate_object_key", nil)
	}
	return nil
}

// isSafeRegularFile 要求文件为 0600、单硬链接普通文件，防止符号链接、设备文件和别名覆盖。
func isSafeRegularFile(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}
