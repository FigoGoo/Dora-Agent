package mediapreview

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	maxPNGBytes int64 = 32 * 1024 * 1024
	maxMP4Bytes int64 = 128 * 1024 * 1024
)

// LocalObjectStore 持有 Business 管理的 0700 本地对象根与固定目录句柄。
type LocalObjectStore struct {
	rootPath string
	root     *os.Root
}

var _ ArtifactStore = (*LocalObjectStore)(nil)

// OpenLocalObjectStore 校验绝对、非符号链接、精确 0700 根，并创建固定 staging/objects 子目录。
func OpenLocalObjectStore(rootPath string) (*LocalObjectStore, error) {
	if rootPath == "" || strings.IndexByte(rootPath, 0) >= 0 || !filepath.IsAbs(rootPath) {
		return nil, ErrDependencyNotReady
	}
	cleanRoot := filepath.Clean(rootPath)
	rootInfo, err := os.Lstat(cleanRoot)
	if err != nil || rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() || rootInfo.Mode().Perm() != 0o700 {
		return nil, ErrDependencyNotReady
	}
	root, err := os.OpenRoot(cleanRoot)
	if err != nil {
		return nil, ErrDependencyNotReady
	}
	store := &LocalObjectStore{rootPath: cleanRoot, root: root}
	openedInfo, err := root.Stat(".")
	if err != nil || !openedInfo.IsDir() || !os.SameFile(rootInfo, openedInfo) {
		_ = root.Close()
		return nil, ErrDependencyNotReady
	}
	for _, directory := range []string{"staging", "objects"} {
		if err := store.ensureDirectory(directory); err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	return store, nil
}

// Close 关闭对象根句柄；关闭后 Store 不得继续服务请求。
func (store *LocalObjectStore) Close() error {
	if store == nil || store.root == nil {
		return nil
	}
	err := store.root.Close()
	store.root = nil
	return err
}

// Ready 重新复核根和两个固定目录仍为安全目录。
func (store *LocalObjectStore) Ready() bool {
	if store == nil || store.root == nil {
		return false
	}
	for _, directory := range []string{".", "staging", "objects"} {
		info, err := store.root.Lstat(directory)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
			return false
		}
	}
	return true
}

// EnsurePreparation 创建并复核本 Asset 的 staging 与 objects 0700 父目录。
func (store *LocalObjectStore) EnsurePreparation(stagingObjectKey string, finalObjectKey string) error {
	if store == nil || store.root == nil || !ValidObjectKey(stagingObjectKey) || !ValidObjectKey(finalObjectKey) ||
		!strings.HasPrefix(stagingObjectKey, "staging/") || !strings.HasPrefix(finalObjectKey, "objects/") {
		return ErrDependencyNotReady
	}
	for _, key := range []string{stagingObjectKey, finalObjectKey} {
		parent := path.Dir(key)
		if path.Dir(parent) != "staging" && path.Dir(parent) != "objects" {
			return ErrInvalidArgument
		}
		if err := store.ensureDirectory(parent); err != nil {
			return err
		}
		if err := store.verifyParents(key); err != nil {
			return err
		}
	}
	return nil
}

// Verify 复核一个 ready Source 或已发布目标的安全 inode、摘要、大小、magic 和固定元数据。
func (store *LocalObjectStore) Verify(objectKey string, expected OutputMetadata) error {
	file, err := store.openSafeRegular(objectKey)
	if err != nil {
		return err
	}
	validationErr := validateOpenArtifact(file, expected)
	closeErr := file.Close()
	if validationErr != nil || closeErr != nil {
		return ErrArtifactInvalid
	}
	return nil
}

// Promote 验证 staging 文件后原子发布到 objects；已存在相同终态文件时按恢复语义收敛。
func (store *LocalObjectStore) Promote(stagingObjectKey string, finalObjectKey string, expected OutputMetadata) error {
	if err := store.EnsurePreparation(stagingObjectKey, finalObjectKey); err != nil {
		return err
	}
	if err := store.Verify(finalObjectKey, expected); err == nil {
		// rename 成功但数据库提交结果未知时，同一命令可从已发布对象恢复。
		return nil
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	if err := store.Verify(stagingObjectKey, expected); err != nil {
		return err
	}
	if err := store.root.Rename(stagingObjectKey, finalObjectKey); err != nil {
		return ErrUnknownOutcome
	}
	if err := store.Verify(finalObjectKey, expected); err != nil {
		return ErrUnknownOutcome
	}
	if err := store.syncDirectory(path.Dir(stagingObjectKey)); err != nil {
		return ErrUnknownOutcome
	}
	if path.Dir(stagingObjectKey) != path.Dir(finalObjectKey) {
		if err := store.syncDirectory(path.Dir(finalObjectKey)); err != nil {
			return ErrUnknownOutcome
		}
	}
	return nil
}

// OpenVerified 打开并在同一个文件句柄上复核 ready 文件，成功时位置重置到起点。
func (store *LocalObjectStore) OpenVerified(objectKey string, expected OutputMetadata) (*os.File, error) {
	file, err := store.openSafeRegular(objectKey)
	if err != nil {
		return nil, err
	}
	if err := validateOpenArtifact(file, expected); err != nil {
		_ = file.Close()
		return nil, ErrArtifactInvalid
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return nil, ErrArtifactInvalid
	}
	return file, nil
}

func (store *LocalObjectStore) ensureDirectory(key string) error {
	if store == nil || store.root == nil || !ValidObjectKey(key) {
		return ErrDependencyNotReady
	}
	err := store.root.Mkdir(key, 0o700)
	if err != nil && !errors.Is(err, fs.ErrExist) {
		return ErrDependencyNotReady
	}
	info, err := store.root.Lstat(key)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return ErrDependencyNotReady
	}
	return nil
}

func (store *LocalObjectStore) verifyParents(key string) error {
	if store == nil || store.root == nil || !ValidObjectKey(key) {
		return ErrInvalidArgument
	}
	components := strings.Split(key, "/")
	current := ""
	for index := 0; index < len(components)-1; index++ {
		if current == "" {
			current = components[index]
		} else {
			current += "/" + components[index]
		}
		info, err := store.root.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
			return ErrArtifactInvalid
		}
	}
	return nil
}

func (store *LocalObjectStore) openSafeRegular(key string) (*os.File, error) {
	if store == nil || store.root == nil || !ValidObjectKey(key) {
		return nil, ErrInvalidArgument
	}
	if err := store.verifyParents(key); err != nil {
		return nil, err
	}
	linkInfo, err := store.root.Lstat(key)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil || !safeRegularFile(linkInfo) {
		return nil, ErrArtifactInvalid
	}
	file, err := store.root.OpenFile(key, os.O_RDONLY, 0)
	if err != nil {
		return nil, ErrArtifactInvalid
	}
	openedInfo, err := file.Stat()
	if err != nil || !safeRegularFile(openedInfo) || !os.SameFile(linkInfo, openedInfo) {
		_ = file.Close()
		return nil, ErrArtifactInvalid
	}
	return file, nil
}

func (store *LocalObjectStore) syncDirectory(key string) error {
	directory, err := store.root.Open(key)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	return errors.Join(syncErr, closeErr)
}

func validateOpenArtifact(file *os.File, expected OutputMetadata) error {
	if file == nil || expected.Validate() != nil {
		return ErrArtifactInvalid
	}
	info, err := file.Stat()
	if err != nil || !safeRegularFile(info) || info.Size() != expected.SizeBytes ||
		(expected.MIMEType == MIMEPNG && info.Size() > maxPNGBytes) ||
		(expected.MIMEType == MIMEMP4 && info.Size() > maxMP4Bytes) {
		return ErrArtifactInvalid
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil || !equalDigest(hash.Sum(nil), expected.ContentDigest) {
		return ErrArtifactInvalid
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return ErrArtifactInvalid
	}
	switch expected.MIMEType {
	case MIMEPNG:
		configuration, err := png.DecodeConfig(file)
		if err != nil || configuration.Width != PNGWidth || configuration.Height != PNGHeight {
			return ErrArtifactInvalid
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return ErrArtifactInvalid
		}
		decoded, err := png.Decode(file)
		if err != nil || decoded.Bounds().Dx() != PNGWidth || decoded.Bounds().Dy() != PNGHeight {
			return ErrArtifactInvalid
		}
	case MIMEMP4:
		if err := validateTopLevelMP4Boxes(file, info.Size()); err != nil {
			return ErrArtifactInvalid
		}
	default:
		return ErrArtifactInvalid
	}
	return nil
}

func validateTopLevelMP4Boxes(reader io.ReaderAt, fileSize int64) error {
	if fileSize < 8 {
		return ErrArtifactInvalid
	}
	var offset int64
	ftypOffset, moovOffset, mdatOffset := int64(-1), int64(-1), int64(-1)
	validFileType := false
	header := make([]byte, 16)
	for offset < fileSize {
		if _, err := reader.ReadAt(header[:8], offset); err != nil {
			return ErrArtifactInvalid
		}
		boxSize := int64(binary.BigEndian.Uint32(header[:4]))
		headerSize := int64(8)
		if boxSize == 1 {
			if _, err := reader.ReadAt(header[8:16], offset+8); err != nil {
				return ErrArtifactInvalid
			}
			boxSize = int64(binary.BigEndian.Uint64(header[8:16]))
			headerSize = 16
		} else if boxSize == 0 {
			boxSize = fileSize - offset
		}
		if boxSize < headerSize || boxSize > fileSize-offset {
			return ErrArtifactInvalid
		}
		switch string(header[4:8]) {
		case "ftyp":
			if ftypOffset < 0 {
				ftypOffset = offset
				if offset == 0 && boxSize >= 16 {
					if _, err := reader.ReadAt(header[:4], offset+8); err == nil {
						switch string(header[:4]) {
						case "isom", "iso2", "avc1", "mp41", "mp42":
							validFileType = true
						}
					}
				}
			}
		case "moov":
			if moovOffset < 0 {
				moovOffset = offset
			}
		case "mdat":
			if mdatOffset < 0 {
				mdatOffset = offset
			}
		}
		offset += boxSize
	}
	if offset != fileSize || ftypOffset != 0 || !validFileType || moovOffset < 0 || mdatOffset < 0 || moovOffset >= mdatOffset {
		return ErrArtifactInvalid
	}
	return nil
}

func safeRegularFile(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}

func equalDigest(actual []byte, expected Digest) bool {
	if len(actual) != sha256.Size {
		return false
	}
	var value Digest
	copy(value[:], actual)
	return value == expected
}
