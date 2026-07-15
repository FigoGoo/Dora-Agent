package reviewfreeze_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	reviewFreezeCompileGitRawBundleMaxCommitObjectsV1 = 1
	reviewFreezeCompileGitRawBundleMaxTreeObjectsV1   = 64
	reviewFreezeCompileGitRawBundleMaxObjectsV1       = reviewFreezeCompileGitRawBundleMaxCommitObjectsV1 + reviewFreezeCompileGitRawBundleMaxTreeObjectsV1
	reviewFreezeCompileGitRawBundleMaxCommitBodyV1    = 1 << 20
	reviewFreezeCompileGitRawBundleMaxTreeBodyV1      = 2 << 20
	reviewFreezeCompileGitRawBundleMaxTreeTotalBodyV1 = 8 << 20
	reviewFreezeCompileGitRawBundleMaxTotalBodyV1     = 9 << 20
)

// reviewFreezeCompileGitRawObjectDescriptorV1 是共享 raw Git CAS 的不可变清单项。
// BodySHA256 只覆盖未压缩 object body；canonical frame 与 Git SHA-1 由 resolver 自行构造。
type reviewFreezeCompileGitRawObjectDescriptorV1 struct {
	ObjectID      string
	Kind          string
	BodySizeBytes int64
	BodySHA256    string
}

// reviewFreezeCompileGitRawObjectOpenedV1 是 CAS 对一次 Open 的原子观察。
// Reader 只返回未压缩 object body；Descriptor 必须与 List 冻结值逐字段相同，Close 必须打断 Read。
type reviewFreezeCompileGitRawObjectOpenedV1 struct {
	Descriptor reviewFreezeCompileGitRawObjectDescriptorV1
	Reader     io.ReadCloser
}

// reviewFreezeCompileGitRawObjectLoaderV1 定义共享 resolver 的最小外部 CAS 边界。
// resolver 恰好 List 一次，并按严格排序后的每个 ObjectID 恰好 Open 一次。
type reviewFreezeCompileGitRawObjectLoaderV1 interface {
	List(context.Context) ([]reviewFreezeCompileGitRawObjectDescriptorV1, error)
	Open(context.Context, string) (reviewFreezeCompileGitRawObjectOpenedV1, error)
}

// reviewFreezeCompileGitRawFrozenObjectV1 保存 resolver 已验证且不可变的对象内容。
// body 使用 string 隔离调用方切片；所有对外 accessor 均重新复制 bytes。
type reviewFreezeCompileGitRawFrozenObjectV1 struct {
	descriptor reviewFreezeCompileGitRawObjectDescriptorV1
	body       string
}

// reviewFreezeCompileGitRawObjectBundleV1 是一次 List/Open 观察后冻结的共享对象集合。
// descriptors 严格按 ObjectID 升序，objects 不再持有或访问外部 CAS。
type reviewFreezeCompileGitRawObjectBundleV1 struct {
	descriptors []reviewFreezeCompileGitRawObjectDescriptorV1
	objects     map[string]reviewFreezeCompileGitRawFrozenObjectV1
}

// Descriptors 返回严格排序 descriptor 的副本；调用方修改返回切片不会污染 bundle。
func (bundle *reviewFreezeCompileGitRawObjectBundleV1) Descriptors() []reviewFreezeCompileGitRawObjectDescriptorV1 {
	if bundle == nil {
		return nil
	}
	return append([]reviewFreezeCompileGitRawObjectDescriptorV1(nil), bundle.descriptors...)
}

// ObjectIDs 返回严格排序的对象 ID 副本，用于后续 exact-set 组合准入。
func (bundle *reviewFreezeCompileGitRawObjectBundleV1) ObjectIDs() []string {
	if bundle == nil {
		return nil
	}
	objectIDs := make([]string, 0, len(bundle.descriptors))
	for _, descriptor := range bundle.descriptors {
		objectIDs = append(objectIDs, descriptor.ObjectID)
	}
	return objectIDs
}

// Descriptor 返回单个 descriptor 的值副本；不存在的 OID 返回 false。
func (bundle *reviewFreezeCompileGitRawObjectBundleV1) Descriptor(objectID string) (reviewFreezeCompileGitRawObjectDescriptorV1, bool) {
	if bundle == nil {
		return reviewFreezeCompileGitRawObjectDescriptorV1{}, false
	}
	object, exists := bundle.objects[objectID]
	return object.descriptor, exists
}

// BodyBytes 返回冻结 body 的字节副本；不存在的 OID 返回 false。
func (bundle *reviewFreezeCompileGitRawObjectBundleV1) BodyBytes(objectID string) ([]byte, bool) {
	object, exists := bundle.object(objectID)
	if !exists {
		return nil, false
	}
	return []byte(object.body), true
}

// CanonicalFrameBytes 由冻结 kind/body 现场构造 frame 副本，不复用 loader 提供的 framing。
func (bundle *reviewFreezeCompileGitRawObjectBundleV1) CanonicalFrameBytes(objectID string) ([]byte, bool) {
	object, exists := bundle.object(objectID)
	if !exists {
		return nil, false
	}
	return reviewFreezeCompileGitRawCanonicalFrameV1(object.descriptor.Kind, []byte(object.body)), true
}

// object 只在本文件内部返回包含 immutable string 的值副本，避免暴露 bundle map。
func (bundle *reviewFreezeCompileGitRawObjectBundleV1) object(objectID string) (reviewFreezeCompileGitRawFrozenObjectV1, bool) {
	if bundle == nil {
		return reviewFreezeCompileGitRawFrozenObjectV1{}, false
	}
	object, exists := bundle.objects[objectID]
	return object, exists
}

// reviewFreezeResolveCompileGitRawObjectBundleV1 先完整验证 List，再逐对象单次 Open。
// 每个 body 经 size+1 有界读取、摘要校验和 canonical frame SHA-1 重算后才进入 bundle。
func reviewFreezeResolveCompileGitRawObjectBundleV1(
	ctx context.Context,
	loader reviewFreezeCompileGitRawObjectLoaderV1,
) (*reviewFreezeCompileGitRawObjectBundleV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("git raw bundle context 不能为空")
	}
	if loader == nil {
		return nil, fmt.Errorf("git raw bundle loader 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("git raw bundle context before List: %w", err)
	}
	listed, err := loader.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("git raw bundle List: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("git raw bundle context after List: %w", err)
	}
	if len(listed) == 0 || len(listed) > reviewFreezeCompileGitRawBundleMaxObjectsV1 {
		return nil, fmt.Errorf("git raw bundle descriptor count=%d limit=1..%d", len(listed), reviewFreezeCompileGitRawBundleMaxObjectsV1)
	}

	// 先限制 slice 长度再复制，避免恶意 List 借 descriptor 数量制造额外内存放大。
	descriptors := append([]reviewFreezeCompileGitRawObjectDescriptorV1(nil), listed...)
	if err := reviewFreezeValidateCompileGitRawDescriptorsV1(descriptors); err != nil {
		return nil, err
	}
	bundle := &reviewFreezeCompileGitRawObjectBundleV1{
		descriptors: append([]reviewFreezeCompileGitRawObjectDescriptorV1(nil), descriptors...),
		objects:     make(map[string]reviewFreezeCompileGitRawFrozenObjectV1, len(descriptors)),
	}
	for _, descriptor := range descriptors {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("git raw bundle context before Open object=%s: %w", descriptor.ObjectID, err)
		}
		opened, err := loader.Open(ctx, descriptor.ObjectID)
		if err != nil {
			if opened.Reader != nil {
				return nil, reviewFreezeCloseCompileGitRawObjectV1(
					opened.Reader,
					fmt.Errorf("git raw bundle Open object=%s: %w", descriptor.ObjectID, err),
				)
			}
			return nil, fmt.Errorf("git raw bundle Open object=%s: %w", descriptor.ObjectID, err)
		}
		if opened.Reader == nil {
			return nil, fmt.Errorf("git raw bundle Open object=%s 返回 nil reader", descriptor.ObjectID)
		}
		if opened.Descriptor != descriptor {
			return nil, reviewFreezeCloseCompileGitRawObjectV1(
				opened.Reader,
				fmt.Errorf("git raw bundle opened descriptor drift object=%s actual=%+v want=%+v", descriptor.ObjectID, opened.Descriptor, descriptor),
			)
		}
		if err := ctx.Err(); err != nil {
			return nil, reviewFreezeCloseCompileGitRawObjectV1(
				opened.Reader,
				fmt.Errorf("git raw bundle context after Open object=%s: %w", descriptor.ObjectID, err),
			)
		}
		body, err := reviewFreezeReadCompileGitRawBodyV1(ctx, opened.Reader, descriptor.ObjectID, descriptor.BodySizeBytes)
		if err != nil {
			return nil, err
		}
		if actualSHA256 := reviewFreezeSHA256V1(body); actualSHA256 != descriptor.BodySHA256 {
			return nil, fmt.Errorf("git raw bundle body SHA-256 drift object=%s actual=%q want=%q", descriptor.ObjectID, actualSHA256, descriptor.BodySHA256)
		}
		frame := reviewFreezeCompileGitRawCanonicalFrameV1(descriptor.Kind, body)
		digest := sha1.Sum(frame)
		if actualObjectID := hex.EncodeToString(digest[:]); actualObjectID != descriptor.ObjectID {
			return nil, fmt.Errorf("git raw bundle canonical SHA-1 drift object=%s actual=%q", descriptor.ObjectID, actualObjectID)
		}
		bundle.objects[descriptor.ObjectID] = reviewFreezeCompileGitRawFrozenObjectV1{
			descriptor: descriptor,
			body:       string(append([]byte(nil), body...)),
		}
	}
	return bundle, nil
}

// reviewFreezeValidateCompileGitRawDescriptorsV1 在任何 Open 前完成排序、类型和预算校验。
// 严格升序同时关闭 duplicate；commit/tree 分别计数，所有声明 body 共用 9 MiB 上限。
func reviewFreezeValidateCompileGitRawDescriptorsV1(descriptors []reviewFreezeCompileGitRawObjectDescriptorV1) error {
	commitCount := 0
	treeCount := 0
	treeBodyBytes := int64(0)
	totalBodyBytes := int64(0)
	previousObjectID := ""
	for index, descriptor := range descriptors {
		if !reviewFreezeGitSHA1V1.MatchString(descriptor.ObjectID) || descriptor.ObjectID == strings.Repeat("0", 40) {
			return fmt.Errorf("git raw bundle descriptor lowercase object_id 非法 index=%d value=%q", index, descriptor.ObjectID)
		}
		if index > 0 && previousObjectID >= descriptor.ObjectID {
			return fmt.Errorf("git raw bundle descriptor 必须严格升序且唯一 previous=%q current=%q", previousObjectID, descriptor.ObjectID)
		}
		previousObjectID = descriptor.ObjectID
		if descriptor.BodySizeBytes < 0 {
			return fmt.Errorf("git raw bundle descriptor body size 非法 object=%s size=%d", descriptor.ObjectID, descriptor.BodySizeBytes)
		}
		switch descriptor.Kind {
		case "commit":
			commitCount++
			if commitCount > reviewFreezeCompileGitRawBundleMaxCommitObjectsV1 {
				return fmt.Errorf("git raw bundle commit object count=%d limit=%d", commitCount, reviewFreezeCompileGitRawBundleMaxCommitObjectsV1)
			}
			if descriptor.BodySizeBytes > reviewFreezeCompileGitRawBundleMaxCommitBodyV1 {
				return fmt.Errorf("git raw bundle commit body budget object=%s size=%d limit=%d", descriptor.ObjectID, descriptor.BodySizeBytes, reviewFreezeCompileGitRawBundleMaxCommitBodyV1)
			}
		case "tree":
			treeCount++
			if treeCount > reviewFreezeCompileGitRawBundleMaxTreeObjectsV1 {
				return fmt.Errorf("git raw bundle tree object count=%d limit=%d", treeCount, reviewFreezeCompileGitRawBundleMaxTreeObjectsV1)
			}
			if descriptor.BodySizeBytes > reviewFreezeCompileGitRawBundleMaxTreeBodyV1 {
				return fmt.Errorf("git raw bundle tree body budget object=%s size=%d limit=%d", descriptor.ObjectID, descriptor.BodySizeBytes, reviewFreezeCompileGitRawBundleMaxTreeBodyV1)
			}
			if descriptor.BodySizeBytes > reviewFreezeCompileGitRawBundleMaxTreeTotalBodyV1-treeBodyBytes {
				return fmt.Errorf("git raw bundle tree total body budget object=%s consumed=%d size=%d limit=%d", descriptor.ObjectID, treeBodyBytes, descriptor.BodySizeBytes, reviewFreezeCompileGitRawBundleMaxTreeTotalBodyV1)
			}
			treeBodyBytes += descriptor.BodySizeBytes
		default:
			return fmt.Errorf("git raw bundle descriptor kind=%q object=%s", descriptor.Kind, descriptor.ObjectID)
		}
		if !reviewFreezePrefixedSHA256V1.MatchString(descriptor.BodySHA256) {
			return fmt.Errorf("git raw bundle descriptor body SHA-256 非法 object=%s digest=%q", descriptor.ObjectID, descriptor.BodySHA256)
		}
		if descriptor.BodySizeBytes > reviewFreezeCompileGitRawBundleMaxTotalBodyV1-totalBodyBytes {
			return fmt.Errorf("git raw bundle total body budget object=%s consumed=%d size=%d limit=%d", descriptor.ObjectID, totalBodyBytes, descriptor.BodySizeBytes, reviewFreezeCompileGitRawBundleMaxTotalBodyV1)
		}
		totalBodyBytes += descriptor.BodySizeBytes
	}
	if commitCount != 1 {
		return fmt.Errorf("git raw bundle 必须恰好包含一个 commit actual=%d", commitCount)
	}
	if treeCount == 0 {
		return fmt.Errorf("git raw bundle 必须至少包含一个 tree")
	}
	return nil
}

// reviewFreezeReadCompileGitRawBodyV1 只读取 descriptor 声明 body size 加一个探测字节。
// context 取消时主动 Close；loader 必须让 Close 及时打断 Read，防止验证 goroutine 悬挂。
func reviewFreezeReadCompileGitRawBodyV1(
	ctx context.Context,
	reader io.ReadCloser,
	objectID string,
	expectedBodyBytes int64,
) ([]byte, error) {
	type readResult struct {
		body []byte
		err  error
	}
	result := make(chan readResult, 1)
	go func() {
		body, err := io.ReadAll(io.LimitReader(reader, expectedBodyBytes+1))
		result <- readResult{body: body, err: err}
	}()
	select {
	case <-ctx.Done():
		closeErr := reader.Close()
		// Reader 契约要求 Close 打断 Read；取消出口必须收割 goroutine，不能把读取遗留到后台。
		read := <-result
		joined := []error{fmt.Errorf("git raw bundle context during read object=%s: %w", objectID, ctx.Err())}
		if read.err != nil {
			joined = append(joined, fmt.Errorf("git raw bundle read after cancel object=%s: %w", objectID, read.err))
		}
		if closeErr != nil {
			joined = append(joined, fmt.Errorf("git raw bundle close after cancel object=%s: %w", objectID, closeErr))
		}
		return nil, errors.Join(joined...)
	case read := <-result:
		closeErr := reader.Close()
		if read.err != nil || closeErr != nil {
			joined := make([]error, 0, 2)
			if read.err != nil {
				joined = append(joined, fmt.Errorf("git raw bundle read object=%s: %w", objectID, read.err))
			}
			if closeErr != nil {
				joined = append(joined, fmt.Errorf("git raw bundle close object=%s: %w", objectID, closeErr))
			}
			return nil, errors.Join(joined...)
		}
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("git raw bundle context after read object=%s: %w", objectID, err)
		}
		if int64(len(read.body)) < expectedBodyBytes {
			return nil, fmt.Errorf("git raw bundle body truncated object=%s actual=%d want=%d", objectID, len(read.body), expectedBodyBytes)
		}
		if int64(len(read.body)) > expectedBodyBytes {
			return nil, fmt.Errorf("git raw bundle body oversized object=%s actual>%d", objectID, expectedBodyBytes)
		}
		return read.body, nil
	}
}

// reviewFreezeCloseCompileGitRawObjectV1 在 metadata/context 失败时关闭 Reader 并保留根因。
func reviewFreezeCloseCompileGitRawObjectV1(reader io.ReadCloser, cause error) error {
	if closeErr := reader.Close(); closeErr != nil {
		return errors.Join(cause, fmt.Errorf("git raw bundle close after failure: %w", closeErr))
	}
	return cause
}

// reviewFreezeCompileGitRawCanonicalFrameV1 只用已验证 kind 和实际 body 长度构造 Git frame。
func reviewFreezeCompileGitRawCanonicalFrameV1(kind string, body []byte) []byte {
	header := kind + " " + strconv.Itoa(len(body))
	frame := make([]byte, 0, len(header)+1+len(body))
	frame = append(frame, header...)
	frame = append(frame, 0)
	frame = append(frame, body...)
	return frame
}

// reviewFreezeCompileGitRawCommitLoaderViewV1 将 bundle 单次投影为旧 commit verifier 契约。
// view 只读取冻结对象并现场构造 frame，不会再次访问底层 CAS。
type reviewFreezeCompileGitRawCommitLoaderViewV1 struct {
	bundle *reviewFreezeCompileGitRawObjectBundleV1
	mu     sync.Mutex
	listed bool
	opened bool
}

// NewCommitObjectLoaderView 创建一个仅允许 List/Open 各一次的 commit 只读视图。
func (bundle *reviewFreezeCompileGitRawObjectBundleV1) NewCommitObjectLoaderView() *reviewFreezeCompileGitRawCommitLoaderViewV1 {
	return &reviewFreezeCompileGitRawCommitLoaderViewV1{bundle: bundle}
}

// ListCommitObjects 投影唯一 commit descriptor；BodySHA256 保持 body 摘要语义。
func (view *reviewFreezeCompileGitRawCommitLoaderViewV1) ListCommitObjects(ctx context.Context) ([]reviewFreezeCompileCommitObjectDescriptorV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("git raw commit view context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.bundle == nil {
		return nil, fmt.Errorf("git raw commit view bundle 不能为空")
	}
	if view.listed {
		return nil, fmt.Errorf("git raw commit view List 只能调用一次")
	}
	view.listed = true
	listed := make([]reviewFreezeCompileCommitObjectDescriptorV1, 0, 1)
	for _, descriptor := range view.bundle.descriptors {
		if descriptor.Kind != "commit" {
			continue
		}
		listed = append(listed, reviewFreezeCompileCommitObjectDescriptorV1{
			ObjectID:      descriptor.ObjectID,
			ObjectKind:    descriptor.Kind,
			BodySizeBytes: descriptor.BodySizeBytes,
			BodySHA256:    descriptor.BodySHA256,
		})
	}
	return listed, nil
}

// OpenCommitObject 返回 bundle 内 commit 的 canonical frame 副本；同一视图拒绝重复 Open。
func (view *reviewFreezeCompileGitRawCommitLoaderViewV1) OpenCommitObject(ctx context.Context, objectID string) (reviewFreezeCompileCommitObjectOpenedV1, error) {
	if ctx == nil {
		return reviewFreezeCompileCommitObjectOpenedV1{}, fmt.Errorf("git raw commit view context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return reviewFreezeCompileCommitObjectOpenedV1{}, err
	}
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.bundle == nil || !view.listed {
		return reviewFreezeCompileCommitObjectOpenedV1{}, fmt.Errorf("git raw commit view 必须先 List")
	}
	if view.opened {
		return reviewFreezeCompileCommitObjectOpenedV1{}, fmt.Errorf("git raw commit view Open 只能调用一次")
	}
	object, exists := view.bundle.object(objectID)
	if !exists || object.descriptor.Kind != "commit" {
		return reviewFreezeCompileCommitObjectOpenedV1{}, fmt.Errorf("git raw commit view object 不存在或类型错误=%q", objectID)
	}
	view.opened = true
	descriptor := reviewFreezeCompileCommitObjectDescriptorV1{
		ObjectID:      object.descriptor.ObjectID,
		ObjectKind:    object.descriptor.Kind,
		BodySizeBytes: object.descriptor.BodySizeBytes,
		BodySHA256:    object.descriptor.BodySHA256,
	}
	frame := reviewFreezeCompileGitRawCanonicalFrameV1(object.descriptor.Kind, []byte(object.body))
	return reviewFreezeCompileCommitObjectOpenedV1{Descriptor: descriptor, Reader: io.NopCloser(bytes.NewReader(frame))}, nil
}

// reviewFreezeCompileGitRawTreeLoaderViewV1 将 bundle 单次投影为旧 tree verifier 契约。
// 每个 tree OID 最多 Open 一次；SHA256 按旧契约覆盖现场构造的完整 frame。
type reviewFreezeCompileGitRawTreeLoaderViewV1 struct {
	bundle *reviewFreezeCompileGitRawObjectBundleV1
	mu     sync.Mutex
	listed bool
	opened map[string]struct{}
}

// NewTreeObjectLoaderView 创建一个单次 List、逐 tree 单次 Open 的只读视图。
func (bundle *reviewFreezeCompileGitRawObjectBundleV1) NewTreeObjectLoaderView() *reviewFreezeCompileGitRawTreeLoaderViewV1 {
	return &reviewFreezeCompileGitRawTreeLoaderViewV1{bundle: bundle, opened: make(map[string]struct{})}
}

// List 投影全部 tree descriptor，并把 body 摘要转换为旧契约要求的完整 frame 摘要。
func (view *reviewFreezeCompileGitRawTreeLoaderViewV1) List(ctx context.Context) ([]reviewFreezeCompileGitObjectDescriptorV1, error) {
	if ctx == nil {
		return nil, fmt.Errorf("git raw tree view context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.bundle == nil {
		return nil, fmt.Errorf("git raw tree view bundle 不能为空")
	}
	if view.listed {
		return nil, fmt.Errorf("git raw tree view List 只能调用一次")
	}
	view.listed = true
	listed := make([]reviewFreezeCompileGitObjectDescriptorV1, 0, len(view.bundle.descriptors))
	for _, descriptor := range view.bundle.descriptors {
		if descriptor.Kind != "tree" {
			continue
		}
		frame, exists := view.bundle.CanonicalFrameBytes(descriptor.ObjectID)
		if !exists {
			return nil, fmt.Errorf("git raw tree view frozen object missing=%s", descriptor.ObjectID)
		}
		listed = append(listed, reviewFreezeCompileGitObjectDescriptorV1{
			ObjectID:         descriptor.ObjectID,
			Kind:             descriptor.Kind,
			DeclaredBodySize: descriptor.BodySizeBytes,
			SHA256:           reviewFreezeSHA256V1(frame),
		})
	}
	return listed, nil
}

// Open 返回指定 tree 的 canonical frame 副本；重复或非 tree OID 均失败关闭。
func (view *reviewFreezeCompileGitRawTreeLoaderViewV1) Open(ctx context.Context, objectID string) (reviewFreezeCompileGitObjectOpenedV1, error) {
	if ctx == nil {
		return reviewFreezeCompileGitObjectOpenedV1{}, fmt.Errorf("git raw tree view context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return reviewFreezeCompileGitObjectOpenedV1{}, err
	}
	view.mu.Lock()
	defer view.mu.Unlock()
	if view.bundle == nil || !view.listed {
		return reviewFreezeCompileGitObjectOpenedV1{}, fmt.Errorf("git raw tree view 必须先 List")
	}
	if _, duplicate := view.opened[objectID]; duplicate {
		return reviewFreezeCompileGitObjectOpenedV1{}, fmt.Errorf("git raw tree view duplicate Open object=%s", objectID)
	}
	object, exists := view.bundle.object(objectID)
	if !exists || object.descriptor.Kind != "tree" {
		return reviewFreezeCompileGitObjectOpenedV1{}, fmt.Errorf("git raw tree view object 不存在或类型错误=%q", objectID)
	}
	view.opened[objectID] = struct{}{}
	frame := reviewFreezeCompileGitRawCanonicalFrameV1(object.descriptor.Kind, []byte(object.body))
	return reviewFreezeCompileGitObjectOpenedV1{ObjectID: objectID, Reader: io.NopCloser(bytes.NewReader(frame))}, nil
}

// 编译期接口断言保证两个适配器持续满足既有 commit/tree verifier 边界。
var (
	_ reviewFreezeCompileCommitObjectLoaderV1 = (*reviewFreezeCompileGitRawCommitLoaderViewV1)(nil)
	_ reviewFreezeCompileGitObjectLoaderV1    = (*reviewFreezeCompileGitRawTreeLoaderViewV1)(nil)
)

// reviewFreezeCompileGitRawFixtureObjectV1 描述测试输入中的 kind/body；OID 和摘要必须由 fixture 工厂重算。
type reviewFreezeCompileGitRawFixtureObjectV1 struct {
	Kind string
	Body []byte
}

// reviewFreezeCompileGitRawLoaderFixtureV1 是线程安全的 body-only 内存 CAS。
// 它记录 List/Open 次数，并允许注入 TOCTOU、读取阻塞和错误以验证失败关闭边界。
type reviewFreezeCompileGitRawLoaderFixtureV1 struct {
	mu                        sync.Mutex
	descriptors               []reviewFreezeCompileGitRawObjectDescriptorV1
	bodies                    map[string][]byte
	openedDescriptorOverrides map[string]reviewFreezeCompileGitRawObjectDescriptorV1
	readerOverrides           map[string]io.ReadCloser
	openErrors                map[string]error
	openErrorReaders          map[string]io.ReadCloser
	listErr                   error
	afterList                 func()
	listCalls                 int
	openCalls                 map[string]int
}

// reviewFreezeCompileGitRawLoaderFixtureNewV1 从 body 推导 canonical OID，并按 OID 升序构造合法清单。
func reviewFreezeCompileGitRawLoaderFixtureNewV1(objects []reviewFreezeCompileGitRawFixtureObjectV1) *reviewFreezeCompileGitRawLoaderFixtureV1 {
	loader := &reviewFreezeCompileGitRawLoaderFixtureV1{
		bodies:                    make(map[string][]byte, len(objects)),
		openedDescriptorOverrides: make(map[string]reviewFreezeCompileGitRawObjectDescriptorV1),
		readerOverrides:           make(map[string]io.ReadCloser),
		openErrors:                make(map[string]error),
		openErrorReaders:          make(map[string]io.ReadCloser),
		openCalls:                 make(map[string]int),
	}
	for _, object := range objects {
		descriptor := reviewFreezeCompileGitRawDescriptorFixtureV1(object.Kind, object.Body)
		loader.descriptors = append(loader.descriptors, descriptor)
		loader.bodies[descriptor.ObjectID] = append([]byte(nil), object.Body...)
	}
	sort.Slice(loader.descriptors, func(left, right int) bool {
		return loader.descriptors[left].ObjectID < loader.descriptors[right].ObjectID
	})
	return loader
}

// List 返回 descriptor 副本；hook 在返回前执行，用于模拟 List 后取消或外部漂移。
func (loader *reviewFreezeCompileGitRawLoaderFixtureV1) List(ctx context.Context) ([]reviewFreezeCompileGitRawObjectDescriptorV1, error) {
	loader.mu.Lock()
	loader.listCalls++
	descriptors := append([]reviewFreezeCompileGitRawObjectDescriptorV1(nil), loader.descriptors...)
	listErr := loader.listErr
	afterList := loader.afterList
	loader.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if listErr != nil {
		return nil, listErr
	}
	if afterList != nil {
		afterList()
	}
	return descriptors, nil
}

// Open 按 OID 返回 body-only Reader，并在同一原子观察中重返 descriptor。
func (loader *reviewFreezeCompileGitRawLoaderFixtureV1) Open(ctx context.Context, objectID string) (reviewFreezeCompileGitRawObjectOpenedV1, error) {
	loader.mu.Lock()
	loader.openCalls[objectID]++
	descriptor, descriptorExists := loader.descriptorLocked(objectID)
	if override, exists := loader.openedDescriptorOverrides[objectID]; exists {
		descriptor = override
		descriptorExists = true
	}
	body, bodyExists := loader.bodies[objectID]
	bodyCopy := append([]byte(nil), body...)
	readerOverride := loader.readerOverrides[objectID]
	openErr := loader.openErrors[objectID]
	openErrorReader := loader.openErrorReaders[objectID]
	loader.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return reviewFreezeCompileGitRawObjectOpenedV1{}, err
	}
	if openErr != nil {
		return reviewFreezeCompileGitRawObjectOpenedV1{Descriptor: descriptor, Reader: openErrorReader}, openErr
	}
	if !descriptorExists || !bodyExists {
		return reviewFreezeCompileGitRawObjectOpenedV1{Descriptor: descriptor}, nil
	}
	if readerOverride != nil {
		return reviewFreezeCompileGitRawObjectOpenedV1{Descriptor: descriptor, Reader: readerOverride}, nil
	}
	return reviewFreezeCompileGitRawObjectOpenedV1{Descriptor: descriptor, Reader: io.NopCloser(bytes.NewReader(bodyCopy))}, nil
}

// descriptorLocked 在持有 mu 时查找当前 List descriptor，供 Open 模拟同一 CAS 观察。
func (loader *reviewFreezeCompileGitRawLoaderFixtureV1) descriptorLocked(objectID string) (reviewFreezeCompileGitRawObjectDescriptorV1, bool) {
	for _, descriptor := range loader.descriptors {
		if descriptor.ObjectID == objectID {
			return descriptor, true
		}
	}
	return reviewFreezeCompileGitRawObjectDescriptorV1{}, false
}

// totalOpenCalls 返回所有 OID 的 Open 次数总和，用于断言 descriptor 失败发生在任何读取前。
func (loader *reviewFreezeCompileGitRawLoaderFixtureV1) totalOpenCalls() int {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	total := 0
	for _, count := range loader.openCalls {
		total += count
	}
	return total
}

// openCallSnapshot 返回 Open 计数副本，证明 adapter 消费 bundle 时没有回访外部 CAS。
func (loader *reviewFreezeCompileGitRawLoaderFixtureV1) openCallSnapshot() map[string]int {
	loader.mu.Lock()
	defer loader.mu.Unlock()
	result := make(map[string]int, len(loader.openCalls))
	for objectID, count := range loader.openCalls {
		result[objectID] = count
	}
	return result
}

// reviewFreezeCompileGitRawDescriptorFixtureV1 从 kind/body 构造与生产契约一致的 descriptor。
func reviewFreezeCompileGitRawDescriptorFixtureV1(kind string, body []byte) reviewFreezeCompileGitRawObjectDescriptorV1 {
	frame := reviewFreezeCompileGitRawCanonicalFrameV1(kind, body)
	digest := sha1.Sum(frame)
	return reviewFreezeCompileGitRawObjectDescriptorV1{
		ObjectID:      hex.EncodeToString(digest[:]),
		Kind:          kind,
		BodySizeBytes: int64(len(body)),
		BodySHA256:    reviewFreezeSHA256V1(body),
	}
}

// reviewFreezeCompileGitRawValidFixtureV1 构造一个 canonical commit 与其空 root tree。
func reviewFreezeCompileGitRawValidFixtureV1() (*reviewFreezeCompileGitRawLoaderFixtureV1, string, string) {
	treeBody := []byte{}
	treeDescriptor := reviewFreezeCompileGitRawDescriptorFixtureV1("tree", treeBody)
	commitBody := reviewFreezeCompileCommitPayloadV1(treeDescriptor.ObjectID)
	commitDescriptor := reviewFreezeCompileGitRawDescriptorFixtureV1("commit", commitBody)
	loader := reviewFreezeCompileGitRawLoaderFixtureNewV1([]reviewFreezeCompileGitRawFixtureObjectV1{
		{Kind: "commit", Body: commitBody},
		{Kind: "tree", Body: treeBody},
	})
	return loader, commitDescriptor.ObjectID, treeDescriptor.ObjectID
}

// reviewFreezeCompileGitRawBlockingReaderV1 模拟只能由 Close 打断的 CAS body stream。
type reviewFreezeCompileGitRawBlockingReaderV1 struct {
	started   chan struct{}
	closed    chan struct{}
	startOnce sync.Once
	closeOnce sync.Once
	mu        sync.Mutex
	closes    int
}

// reviewFreezeCompileGitRawBlockingReaderNewV1 创建带 started/closed 观测点的阻塞 Reader。
func reviewFreezeCompileGitRawBlockingReaderNewV1() *reviewFreezeCompileGitRawBlockingReaderV1 {
	return &reviewFreezeCompileGitRawBlockingReaderV1{started: make(chan struct{}), closed: make(chan struct{})}
}

// Read 阻塞至 Close，模拟网络或子进程管道在取消时需要主动关闭。
func (reader *reviewFreezeCompileGitRawBlockingReaderV1) Read([]byte) (int, error) {
	reader.startOnce.Do(func() { close(reader.started) })
	<-reader.closed
	return 0, io.ErrClosedPipe
}

// Close 解除阻塞并记录调用次数；重复 Close 不会重复关闭 channel。
func (reader *reviewFreezeCompileGitRawBlockingReaderV1) Close() error {
	reader.mu.Lock()
	reader.closes++
	reader.mu.Unlock()
	reader.closeOnce.Do(func() { close(reader.closed) })
	return nil
}

// closeCount 返回 Close 调用次数的线程安全快照。
func (reader *reviewFreezeCompileGitRawBlockingReaderV1) closeCount() int {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.closes
}

// reviewFreezeCompileGitRawTrackingReaderV1 记录错误路径是否兑现 Close。
type reviewFreezeCompileGitRawTrackingReaderV1 struct {
	reader   io.Reader
	readErr  error
	closeErr error
	mu       sync.Mutex
	closed   int
}

// Read 代理底层 body Reader。
func (reader *reviewFreezeCompileGitRawTrackingReaderV1) Read(buffer []byte) (int, error) {
	if reader.readErr != nil {
		return 0, reader.readErr
	}
	return reader.reader.Read(buffer)
}

// Close 记录关闭，不改变内存 Reader 行为。
func (reader *reviewFreezeCompileGitRawTrackingReaderV1) Close() error {
	reader.mu.Lock()
	reader.closed++
	reader.mu.Unlock()
	return reader.closeErr
}

// closeCount 返回 tracking Reader 的关闭次数。
func (reader *reviewFreezeCompileGitRawTrackingReaderV1) closeCount() int {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	return reader.closed
}

// reviewFreezeCompileGitRawSplitFrameFixtureV1 仅在 fixture 阶段把 git cat-file raw frame 拆成 body。
func reviewFreezeCompileGitRawSplitFrameFixtureV1(t *testing.T, expectedKind string, frame []byte) []byte {
	t.Helper()
	nul := bytes.IndexByte(frame, 0)
	if nul <= 0 {
		t.Fatalf("fixture Git frame missing header kind=%s", expectedKind)
	}
	body := append([]byte(nil), frame[nul+1:]...)
	expectedHeader := expectedKind + " " + strconv.Itoa(len(body))
	if string(frame[:nul]) != expectedHeader {
		t.Fatalf("fixture Git frame header=%q want=%q", frame[:nul], expectedHeader)
	}
	return body
}

// reviewFreezeCompileGitRawEqualStringIntMapV1 比较 Open 计数快照，避免测试依赖反射。
func reviewFreezeCompileGitRawEqualStringIntMapV1(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}

// reviewFreezeCompileGitRawEqualStringsV1 比较已排序的 exact object set。
func reviewFreezeCompileGitRawEqualStringsV1(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// TestW2ReviewFreezeCompileGitRawObjectBundleV1HEADTreeCleanWorktreeUnifiedGate 证明同一次
// body-only CAS 观察可驱动 HEAD commit/tree binding 与 clean worktree 14 个 registered leaf gate；
// leaf bytes 并非从 HEAD blob 读取，因此本测试不冒充脱离 worktree 的纯 HEAD golden。
func TestW2ReviewFreezeCompileGitRawObjectBundleV1HEADTreeCleanWorktreeUnifiedGate(t *testing.T) {
	fixture := reviewFreezeCompileGitHEADTreeCleanWorktreeFixtureV1(t)
	root := reviewFreezeRepoRootV1(t)
	commitBody := []byte(reviewFreezeRunTestGitV1(t, root, "cat-file", "commit", fixture.Statement.Subject.BaseCommitSHA))
	objects := []reviewFreezeCompileGitRawFixtureObjectV1{{Kind: "commit", Body: commitBody}}
	commitDescriptor := reviewFreezeCompileGitRawDescriptorFixtureV1("commit", commitBody)
	if commitDescriptor.ObjectID != fixture.Statement.Subject.BaseCommitSHA {
		t.Fatalf("HEAD/worktree gate commit body OID=%q want=%q", commitDescriptor.ObjectID, fixture.Statement.Subject.BaseCommitSHA)
	}
	for objectID, frame := range fixture.Objects {
		body := reviewFreezeCompileGitRawSplitFrameFixtureV1(t, "tree", frame)
		descriptor := reviewFreezeCompileGitRawDescriptorFixtureV1("tree", body)
		if descriptor.ObjectID != objectID {
			t.Fatalf("HEAD/worktree gate tree body OID=%q want=%q", descriptor.ObjectID, objectID)
		}
		objects = append(objects, reviewFreezeCompileGitRawFixtureObjectV1{Kind: "tree", Body: body})
	}
	loader := reviewFreezeCompileGitRawLoaderFixtureNewV1(objects)
	bundle, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), loader)
	if err != nil {
		t.Fatalf("resolve HEAD/worktree raw bundle: %v", err)
	}
	if loader.listCalls != 1 || len(bundle.Descriptors()) != len(objects) {
		t.Fatalf("HEAD/worktree bundle list=%d descriptors=%d objects=%d", loader.listCalls, len(bundle.Descriptors()), len(objects))
	}
	for _, descriptor := range bundle.Descriptors() {
		if loader.openCalls[descriptor.ObjectID] != 1 {
			t.Fatalf("HEAD/worktree underlying Open object=%s calls=%d", descriptor.ObjectID, loader.openCalls[descriptor.ObjectID])
		}
	}
	openBeforeViews := loader.openCallSnapshot()

	commitBinding, err := reviewFreezeVerifyCompileCommitObjectBindingV1(
		context.Background(),
		fixture.Statement,
		bundle.NewCommitObjectLoaderView(),
	)
	if err != nil {
		t.Fatalf("HEAD/worktree commit view rejected: %v", err)
	}
	treeMembership, err := reviewFreezeVerifyCompileGitBaseTreeMembershipV1(
		context.Background(),
		fixture.SnapshotRaw,
		fixture.Statement,
		fixture.Leaves,
		bundle.NewTreeObjectLoaderView(),
	)
	if err != nil {
		t.Fatalf("HEAD/worktree tree view rejected: %v", err)
	}
	if commitBinding.CommitSHA() != fixture.Statement.Subject.BaseCommitSHA ||
		commitBinding.TreeSHA() != fixture.Statement.Subject.BaseTreeSHA ||
		treeMembership.BaseTreeSHA() != fixture.Statement.Subject.BaseTreeSHA {
		t.Fatalf("HEAD/worktree shared projection drift commit=%s tree=%s membership=%s", commitBinding.CommitSHA(), commitBinding.TreeSHA(), treeMembership.BaseTreeSHA())
	}
	usedObjectIDs := append(commitBinding.UsedObjectIDs(), treeMembership.ObjectIDs()...)
	sort.Strings(usedObjectIDs)
	if !reviewFreezeCompileGitRawEqualStringsV1(usedObjectIDs, bundle.ObjectIDs()) {
		t.Fatalf("HEAD/worktree exact object set used=%v listed=%v", usedObjectIDs, bundle.ObjectIDs())
	}
	if openAfterViews := loader.openCallSnapshot(); !reviewFreezeCompileGitRawEqualStringIntMapV1(openBeforeViews, openAfterViews) {
		t.Fatalf("views revisited external CAS before=%v after=%v", openBeforeViews, openAfterViews)
	}
}

// TestW2ReviewFreezeCompileGitRawObjectBundleV1RejectsDescriptorAdversaries 确认
// 排序、唯一性、类型、数量和声明预算均在任何 Open 前失败关闭。
func TestW2ReviewFreezeCompileGitRawObjectBundleV1RejectsDescriptorAdversaries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*reviewFreezeCompileGitRawLoaderFixtureV1)
	}{
		{name: "empty List", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors = nil
		}},
		{name: "unsorted", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors[0], loader.descriptors[1] = loader.descriptors[1], loader.descriptors[0]
		}},
		{name: "duplicate", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors = append(loader.descriptors, loader.descriptors[len(loader.descriptors)-1])
		}},
		{name: "uppercase OID", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors[0].ObjectID = strings.ToUpper(loader.descriptors[0].ObjectID)
		}},
		{name: "zero OID", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors[0].ObjectID = strings.Repeat("0", 40)
		}},
		{name: "extra commit", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			body := reviewFreezeCompileCommitPayloadV1(strings.Repeat("1", 40), strings.Repeat("2", 40))
			descriptor := reviewFreezeCompileGitRawDescriptorFixtureV1("commit", body)
			loader.descriptors = append(loader.descriptors, descriptor)
			loader.bodies[descriptor.ObjectID] = body
			sort.Slice(loader.descriptors, func(left, right int) bool {
				return loader.descriptors[left].ObjectID < loader.descriptors[right].ObjectID
			})
		}},
		{name: "missing commit", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			filtered := loader.descriptors[:0]
			for _, descriptor := range loader.descriptors {
				if descriptor.Kind == "tree" {
					filtered = append(filtered, descriptor)
				}
			}
			loader.descriptors = filtered
		}},
		{name: "missing tree", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			filtered := loader.descriptors[:0]
			for _, descriptor := range loader.descriptors {
				if descriptor.Kind == "commit" {
					filtered = append(filtered, descriptor)
				}
			}
			loader.descriptors = filtered
		}},
		{name: "unknown kind", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors[0].Kind = "blob"
		}},
		{name: "negative size", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors[0].BodySizeBytes = -1
		}},
		{name: "malformed SHA-256", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors[0].BodySHA256 = "sha256:not-a-digest"
		}},
		{name: "commit body budget", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			for index := range loader.descriptors {
				if loader.descriptors[index].Kind == "commit" {
					loader.descriptors[index].BodySizeBytes = reviewFreezeCompileGitRawBundleMaxCommitBodyV1 + 1
				}
			}
		}},
		{name: "tree body budget", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			for index := range loader.descriptors {
				if loader.descriptors[index].Kind == "tree" {
					loader.descriptors[index].BodySizeBytes = reviewFreezeCompileGitRawBundleMaxTreeBodyV1 + 1
				}
			}
		}},
		{name: "tree object count", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors = make([]reviewFreezeCompileGitRawObjectDescriptorV1, reviewFreezeCompileGitRawBundleMaxTreeObjectsV1+1)
			for index := range loader.descriptors {
				loader.descriptors[index] = reviewFreezeCompileGitRawObjectDescriptorV1{
					ObjectID:      fmt.Sprintf("%040x", index+1),
					Kind:          "tree",
					BodySizeBytes: 0,
					BodySHA256:    reviewFreezeSHA256V1(nil),
				}
			}
		}},
		{name: "tree total body budget", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors = make([]reviewFreezeCompileGitRawObjectDescriptorV1, 6)
			loader.descriptors[0] = reviewFreezeCompileGitRawObjectDescriptorV1{
				ObjectID:      fmt.Sprintf("%040x", 1),
				Kind:          "commit",
				BodySizeBytes: 1,
				BodySHA256:    reviewFreezeSHA256V1([]byte("c")),
			}
			for index := 1; index < len(loader.descriptors); index++ {
				size := int64(reviewFreezeCompileGitRawBundleMaxTreeBodyV1)
				if index == len(loader.descriptors)-1 {
					size = 1
				}
				loader.descriptors[index] = reviewFreezeCompileGitRawObjectDescriptorV1{
					ObjectID:      fmt.Sprintf("%040x", index+1),
					Kind:          "tree",
					BodySizeBytes: size,
					BodySHA256:    reviewFreezeSHA256V1(nil),
				}
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader, _, _ := reviewFreezeCompileGitRawValidFixtureV1()
			test.mutate(loader)
			if _, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), loader); err == nil {
				t.Fatal("descriptor adversary unexpectedly accepted")
			}
			if loader.listCalls != 1 || loader.totalOpenCalls() != 0 {
				t.Fatalf("descriptor failure must precede Open list=%d open=%d", loader.listCalls, loader.totalOpenCalls())
			}
		})
	}
}

// TestW2ReviewFreezeCompileGitRawObjectBundleV1RejectsOpenAndBodyAdversaries 覆盖
// Open 后 descriptor TOCTOU、size、body 摘要和 canonical OID 漂移。
func TestW2ReviewFreezeCompileGitRawObjectBundleV1RejectsOpenAndBodyAdversaries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*reviewFreezeCompileGitRawLoaderFixtureV1)
	}{
		{name: "opened descriptor TOCTOU", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			descriptor := loader.descriptors[0]
			descriptor.BodySHA256 = reviewFreezeSHA256V1([]byte("toctou"))
			loader.openedDescriptorOverrides[descriptor.ObjectID] = descriptor
		}},
		{name: "truncated body", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors[0].BodySizeBytes++
		}},
		{name: "oversized body", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			if loader.descriptors[0].BodySizeBytes == 0 {
				loader.descriptors[0], loader.descriptors[1] = loader.descriptors[1], loader.descriptors[0]
			}
			loader.descriptors[0].BodySizeBytes--
		}},
		{name: "body SHA drift", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			loader.descriptors[0].BodySHA256 = reviewFreezeSHA256V1([]byte("digest drift"))
		}},
		{name: "canonical OID drift", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			descriptor := loader.descriptors[0]
			body := append([]byte(nil), loader.bodies[descriptor.ObjectID]...)
			if len(body) == 0 {
				body = []byte("x")
				descriptor.BodySizeBytes = 1
			} else {
				body[0] ^= 1
			}
			descriptor.BodySHA256 = reviewFreezeSHA256V1(body)
			loader.descriptors[0] = descriptor
			loader.bodies[descriptor.ObjectID] = body
		}},
		{name: "nil reader", mutate: func(loader *reviewFreezeCompileGitRawLoaderFixtureV1) {
			descriptor := loader.descriptors[0]
			delete(loader.bodies, descriptor.ObjectID)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			loader, _, _ := reviewFreezeCompileGitRawValidFixtureV1()
			test.mutate(loader)
			if _, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), loader); err == nil {
				t.Fatal("Open/body adversary unexpectedly accepted")
			}
			if loader.listCalls != 1 || loader.totalOpenCalls() == 0 {
				t.Fatalf("Open/body failure lifecycle list=%d open=%d", loader.listCalls, loader.totalOpenCalls())
			}
		})
	}
}

// TestW2ReviewFreezeCompileGitRawObjectBundleV1TOCTOUClosesReader 证明 metadata 漂移不会泄漏 Reader。
func TestW2ReviewFreezeCompileGitRawObjectBundleV1TOCTOUClosesReader(t *testing.T) {
	loader, _, _ := reviewFreezeCompileGitRawValidFixtureV1()
	descriptor := loader.descriptors[0]
	override := descriptor
	override.Kind = "tree"
	loader.openedDescriptorOverrides[descriptor.ObjectID] = override
	closeSentinel := errors.New("metadata close failed")
	tracking := &reviewFreezeCompileGitRawTrackingReaderV1{reader: bytes.NewReader(loader.bodies[descriptor.ObjectID]), closeErr: closeSentinel}
	loader.readerOverrides[descriptor.ObjectID] = tracking
	if _, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), loader); err == nil {
		t.Fatal("TOCTOU descriptor unexpectedly accepted")
	} else if !errors.Is(err, closeSentinel) {
		t.Fatalf("TOCTOU error 未保留 Close 根因: %v", err)
	}
	if tracking.closeCount() != 1 {
		t.Fatalf("TOCTOU reader close=%d want=1", tracking.closeCount())
	}
}

// TestW2ReviewFreezeCompileGitRawObjectBundleV1OpenErrorClosesReader 证明 error+Reader 也会关闭资源。
func TestW2ReviewFreezeCompileGitRawObjectBundleV1OpenErrorClosesReader(t *testing.T) {
	loader, _, _ := reviewFreezeCompileGitRawValidFixtureV1()
	descriptor := loader.descriptors[0]
	openSentinel := errors.New("CAS unavailable")
	closeSentinel := errors.New("Open error close failed")
	tracking := &reviewFreezeCompileGitRawTrackingReaderV1{reader: bytes.NewReader(nil), closeErr: closeSentinel}
	loader.openErrors[descriptor.ObjectID] = openSentinel
	loader.openErrorReaders[descriptor.ObjectID] = tracking
	if _, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), loader); err == nil {
		t.Fatal("Open error unexpectedly accepted")
	} else if !errors.Is(err, openSentinel) || !errors.Is(err, closeSentinel) {
		t.Fatalf("Open error 未同时保留 Open/Close 根因: %v", err)
	}
	if tracking.closeCount() != 1 {
		t.Fatalf("Open error reader close=%d want=1", tracking.closeCount())
	}
}

// TestW2ReviewFreezeCompileGitRawObjectBundleV1JoinsReadAndCloseErrors 证明双失败不会覆盖首个根因。
func TestW2ReviewFreezeCompileGitRawObjectBundleV1JoinsReadAndCloseErrors(t *testing.T) {
	loader, _, _ := reviewFreezeCompileGitRawValidFixtureV1()
	descriptor := loader.descriptors[0]
	readSentinel := errors.New("body read failed")
	closeSentinel := errors.New("body close failed")
	tracking := &reviewFreezeCompileGitRawTrackingReaderV1{
		reader:   bytes.NewReader(nil),
		readErr:  readSentinel,
		closeErr: closeSentinel,
	}
	loader.readerOverrides[descriptor.ObjectID] = tracking
	_, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), loader)
	if err == nil || !errors.Is(err, readSentinel) || !errors.Is(err, closeSentinel) {
		t.Fatalf("Read/Close 双失败根因未保留: %v", err)
	}
	if tracking.closeCount() != 1 {
		t.Fatalf("Read/Close 双失败 close=%d want=1", tracking.closeCount())
	}
}

// TestW2ReviewFreezeCompileGitRawObjectBundleV1CancellationClosesBlockingReader 证明
// context 取消会主动 Close body Reader，并且 resolver 有界返回。
func TestW2ReviewFreezeCompileGitRawObjectBundleV1CancellationClosesBlockingReader(t *testing.T) {
	loader, _, _ := reviewFreezeCompileGitRawValidFixtureV1()
	objectID := loader.descriptors[0].ObjectID
	blocking := reviewFreezeCompileGitRawBlockingReaderNewV1()
	loader.readerOverrides[objectID] = blocking
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := reviewFreezeResolveCompileGitRawObjectBundleV1(ctx, loader)
		done <- err
	}()
	select {
	case <-blocking.started:
	case <-time.After(time.Second):
		t.Fatal("blocking Reader 未开始读取")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error=%v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("resolver 未在取消后有界返回")
	}
	if blocking.closeCount() != 1 {
		t.Fatalf("blocking Reader close=%d want=1", blocking.closeCount())
	}
}

// TestW2ReviewFreezeCompileGitRawObjectBundleV1ImmutableAccessorsAndSingleUseViews 验证
// accessor/view 只消费冻结副本，调用方 mutation 与底层 CAS mutation 均不能改变 bundle。
func TestW2ReviewFreezeCompileGitRawObjectBundleV1ImmutableAccessorsAndSingleUseViews(t *testing.T) {
	loader, commitID, treeID := reviewFreezeCompileGitRawValidFixtureV1()
	bundle, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), loader)
	if err != nil {
		t.Fatalf("resolve valid raw bundle: %v", err)
	}
	openBeforeViews := loader.openCallSnapshot()
	descriptors := bundle.Descriptors()
	descriptors[0].Kind = "blob"
	if bundle.Descriptors()[0].Kind == "blob" {
		t.Fatal("descriptor accessor 暴露内部切片")
	}
	objectIDs := bundle.ObjectIDs()
	objectIDs[0] = strings.Repeat("f", 40)
	if bundle.ObjectIDs()[0] == strings.Repeat("f", 40) {
		t.Fatal("ObjectIDs accessor 暴露内部切片")
	}
	body, exists := bundle.BodyBytes(commitID)
	if !exists || len(body) == 0 {
		t.Fatalf("commit body missing exists=%v len=%d", exists, len(body))
	}
	originalFirstBodyByte := body[0]
	body[0] ^= 1
	bodyAgain, _ := bundle.BodyBytes(commitID)
	if bodyAgain[0] != originalFirstBodyByte {
		t.Fatal("body accessor mutation 污染 bundle")
	}
	frame, exists := bundle.CanonicalFrameBytes(treeID)
	if !exists || len(frame) == 0 {
		t.Fatalf("tree frame missing exists=%v len=%d", exists, len(frame))
	}
	originalFirstFrameByte := frame[0]
	frame[0] ^= 1
	frameAgain, _ := bundle.CanonicalFrameBytes(treeID)
	if frameAgain[0] != originalFirstFrameByte {
		t.Fatal("frame accessor mutation 污染 bundle")
	}
	loader.bodies[commitID] = []byte("external CAS drift after resolve")
	loader.descriptors[0].Kind = "blob"
	if bodyAfterCASDrift, _ := bundle.BodyBytes(commitID); !bytes.Equal(bodyAfterCASDrift, bodyAgain) {
		t.Fatal("bundle 仍受底层 CAS mutation 影响")
	}

	statement := reviewFreezeCompileAttestationFixtureStatementV1(t)
	statement.Subject.BaseCommitSHA = commitID
	statement.Subject.BaseTreeSHA = treeID
	commitView := bundle.NewCommitObjectLoaderView()
	binding, err := reviewFreezeVerifyCompileCommitObjectBindingV1(context.Background(), statement, commitView)
	if err != nil {
		t.Fatalf("commit single-use view rejected: %v", err)
	}
	if binding.CommitSHA() != commitID || binding.TreeSHA() != treeID {
		t.Fatalf("commit single-use binding=%s/%s want=%s/%s", binding.CommitSHA(), binding.TreeSHA(), commitID, treeID)
	}
	if _, err := commitView.ListCommitObjects(context.Background()); err == nil {
		t.Fatal("commit view repeated List unexpectedly accepted")
	}
	treeView := bundle.NewTreeObjectLoaderView()
	listedTrees, err := treeView.List(context.Background())
	if err != nil || len(listedTrees) != 1 || listedTrees[0].ObjectID != treeID {
		t.Fatalf("tree view List=%+v err=%v", listedTrees, err)
	}
	openedTree, err := treeView.Open(context.Background(), treeID)
	if err != nil {
		t.Fatalf("tree view Open: %v", err)
	}
	if _, err := io.ReadAll(openedTree.Reader); err != nil {
		t.Fatalf("read tree view frame: %v", err)
	}
	if err := openedTree.Reader.Close(); err != nil {
		t.Fatalf("close tree view frame: %v", err)
	}
	if _, err := treeView.Open(context.Background(), treeID); err == nil {
		t.Fatal("tree view repeated Open unexpectedly accepted")
	}
	if openAfterViews := loader.openCallSnapshot(); !reviewFreezeCompileGitRawEqualStringIntMapV1(openBeforeViews, openAfterViews) {
		t.Fatalf("immutable views revisited external CAS before=%v after=%v", openBeforeViews, openAfterViews)
	}
}

// TestW2ReviewFreezeCompileGitRawObjectBundleV1ContextAndLoaderLifecycle 覆盖 nil、预取消和 List error。
func TestW2ReviewFreezeCompileGitRawObjectBundleV1ContextAndLoaderLifecycle(t *testing.T) {
	loader, _, _ := reviewFreezeCompileGitRawValidFixtureV1()
	if _, err := reviewFreezeResolveCompileGitRawObjectBundleV1(nil, loader); err == nil {
		t.Fatal("nil context unexpectedly accepted")
	}
	if _, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), nil); err == nil {
		t.Fatal("nil loader unexpectedly accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reviewFreezeResolveCompileGitRawObjectBundleV1(ctx, loader); !errors.Is(err, context.Canceled) {
		t.Fatalf("pre-canceled context error=%v", err)
	}
	if loader.listCalls != 0 || loader.totalOpenCalls() != 0 {
		t.Fatalf("pre-canceled lifecycle list=%d open=%d", loader.listCalls, loader.totalOpenCalls())
	}
	listErrorLoader, _, _ := reviewFreezeCompileGitRawValidFixtureV1()
	listErrorLoader.listErr = errors.New("CAS List unavailable")
	if _, err := reviewFreezeResolveCompileGitRawObjectBundleV1(context.Background(), listErrorLoader); err == nil {
		t.Fatal("List error unexpectedly accepted")
	}
	if listErrorLoader.listCalls != 1 || listErrorLoader.totalOpenCalls() != 0 {
		t.Fatalf("List error lifecycle list=%d open=%d", listErrorLoader.listCalls, listErrorLoader.totalOpenCalls())
	}
}
