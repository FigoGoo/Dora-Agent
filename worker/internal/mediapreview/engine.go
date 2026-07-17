package mediapreview

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const readinessTimeout = 5 * time.Second

var minimalProcessEnvironment = []string{
	"LANG=C",
	"LC_ALL=C",
	"AV_LOG_FORCE_NOCOLOR=1",
}

// Config 是媒体预览产物引擎的启动后不可变配置。
type Config struct {
	// Profile 必须显式等于 media.runtime.v3preview1，防止误用于生产 Profile。
	Profile RuntimeProfile
	// ObjectRoot 是 Business 管理的本机绝对对象根，必须是非符号链接 0700 目录。
	ObjectRoot string
	// GeneratorVersion 是确定性 PNG 算法版本，当前只允许 png_640x360.deterministic.v1。
	GeneratorVersion GeneratorVersion
	// FFMPEGPath 是可选的 ffmpeg 绝对普通可执行文件；启用 MP4 时与 FFprobePath 同时配置。
	FFMPEGPath string
	// FFprobePath 是可选的 ffprobe 绝对普通可执行文件；启用 MP4 时与 FFMPEGPath 同时配置。
	FFprobePath string
	// StderrLimitBytes 是单次外部命令最多保留的 stderr 字节数，超出部分丢弃且不进入回执。
	StderrLimitBytes int64
}

// ArtifactEngine 是后续 Job Processor 所依赖的最小产物执行接口。
//
// Processor 负责 Claim、Heartbeat、Finalize 和 Terminal；本接口只生成并验证一个本地 artifact。
type ArtifactEngine interface {
	// GeneratePNG 使用冻结 Envelope 生成并原子发布一个确定性 640x360 PNG staging 文件。
	GeneratePNG(ctx context.Context, envelope MediaJobEnvelopeV1) (ArtifactReceiptV1, error)
	// AssembleMP4 使用冻结 Envelope 和固定 ffmpeg argv 生成并验证一个 2 秒 MP4 staging 文件。
	AssembleMP4(ctx context.Context, envelope MediaJobEnvelopeV1) (ArtifactReceiptV1, error)
	// Close 释放对象根目录句柄；调用方必须先停止新的产物执行并等待当前调用返回。
	Close() error
}

// Engine 实现本地 Development Preview 的确定性 PNG 和固定参数 MP4 产物执行。
type Engine struct {
	// store 把所有文件系统访问限制在启动时固定的 Business 对象根内。
	store *objectStore
	// profile 是启动时已经校验的 local-only Runtime Profile。
	profile RuntimeProfile
	// generatorVersion 冻结 PNG 像素派生与编码算法版本。
	generatorVersion GeneratorVersion
	// ffmpegPath 是 readiness 已通过的绝对普通可执行文件；空值表示 PNG-only。
	ffmpegPath string
	// ffprobePath 是 readiness 已通过的绝对普通可执行文件；空值表示 PNG-only。
	ffprobePath string
	// stderrLimit 是每个外部命令的进程内诊断保留上限。
	stderrLimit int64
	// mediaSlot 把单个 Engine 的 ffmpeg/ffprobe 下游并发固定为 1。
	mediaSlot chan struct{}
}

// NewEngine 校验 Profile、对象根和可选媒体二进制，并在返回前执行 ffmpeg/ffprobe readiness。
//
// ffmpeg 与 ffprobe 均为空时只启用 PNG；任一已配置时必须两者齐全、为绝对普通可执行文件且支持 libx264。
func NewEngine(ctx context.Context, config Config) (*Engine, error) {
	if ctx == nil || config.Profile != RuntimeProfileMediaV3Preview1 ||
		config.GeneratorVersion != GeneratorVersionPNG640x360V1 {
		return nil, newArtifactError(ErrorCodeUnsupportedProfile, "validate_engine_config", nil)
	}
	stderrLimit := config.StderrLimitBytes
	if stderrLimit == 0 {
		stderrLimit = DefaultStderrLimitBytes
	}
	if stderrLimit < 1024 || stderrLimit > MaxStderrLimitBytes {
		return nil, newArtifactError(ErrorCodeInvalidArgument, "validate_engine_config", nil)
	}
	if (config.FFMPEGPath == "") != (config.FFprobePath == "") {
		return nil, newArtifactError(ErrorCodeFFmpegUnavailable, "validate_media_tools", nil)
	}

	store, err := newObjectStore(config.ObjectRoot)
	if err != nil {
		return nil, err
	}
	engine := &Engine{
		store:            store,
		profile:          config.Profile,
		generatorVersion: config.GeneratorVersion,
		stderrLimit:      stderrLimit,
		mediaSlot:        make(chan struct{}, 1),
	}

	if config.FFMPEGPath != "" {
		if err := validateExecutable(config.FFMPEGPath); err != nil {
			_ = store.Close()
			return nil, err
		}
		if err := validateExecutable(config.FFprobePath); err != nil {
			_ = store.Close()
			return nil, err
		}
		engine.ffmpegPath = filepath.Clean(config.FFMPEGPath)
		engine.ffprobePath = filepath.Clean(config.FFprobePath)
		if err := engine.validateMediaReadiness(ctx); err != nil {
			_ = store.Close()
			return nil, err
		}
	}
	return engine, nil
}

// Close 释放 Engine 持有的对象根目录句柄。
func (e *Engine) Close() error {
	if e == nil || e.store == nil {
		return nil
	}
	return e.store.Close()
}

// validateMediaReadiness 以最小环境和内部超时确认 ffmpeg 支持 libx264 且 ffprobe 可执行。
func (e *Engine) validateMediaReadiness(ctx context.Context) error {
	readinessContext, cancel := context.WithTimeout(ctx, readinessTimeout)
	defer cancel()

	stdout, stderr, truncated, err := runCapturedCommand(
		readinessContext,
		e.ffmpegPath,
		[]string{"-nostdin", "-hide_banner", "-encoders"},
		e.store.rootPath,
		MaxStderrLimitBytes,
	)
	if err != nil || truncated || !bytes.Contains(append(stdout, stderr...), []byte("libx264")) {
		return newArtifactError(ErrorCodeFFmpegUnavailable, "ffmpeg_readiness", err)
	}

	_, _, truncated, err = runCapturedCommand(
		readinessContext,
		e.ffprobePath,
		[]string{"-v", "error", "-version"},
		e.store.rootPath,
		e.stderrLimit,
	)
	if err != nil || truncated {
		return newArtifactError(ErrorCodeFFmpegUnavailable, "ffprobe_readiness", err)
	}
	return nil
}

// acquireMediaSlot 将 ffmpeg/ffprobe 并发固定为每个 Engine 一个 Job，并服从调用 Context 取消。
func (e *Engine) acquireMediaSlot(ctx context.Context) error {
	select {
	case e.mediaSlot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return newArtifactError(ErrorCodeExecutionTimeout, "acquire_media_slot", ctx.Err())
	}
}

// releaseMediaSlot 释放当前 Engine 唯一的媒体命令并发槽。
func (e *Engine) releaseMediaSlot() {
	<-e.mediaSlot
}

// validateExecutable 校验命令路径是绝对、非符号链接、普通且至少具有一个执行位的文件。
func validateExecutable(executablePath string) error {
	if executablePath == "" || strings.IndexByte(executablePath, 0) >= 0 || !filepath.IsAbs(executablePath) {
		return newArtifactError(ErrorCodeFFmpegUnavailable, "validate_media_executable", nil)
	}
	info, err := os.Lstat(filepath.Clean(executablePath))
	if err != nil {
		return newArtifactError(ErrorCodeFFmpegUnavailable, "validate_media_executable", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return newArtifactError(ErrorCodeFFmpegUnavailable, "validate_media_executable", nil)
	}
	return nil
}

// runCapturedCommand 直接执行绝对二进制并对 stdout/stderr 分别施加相同字节上限。
//
// 命令不经 Shell、不继承宿主环境；返回的诊断只供当前方法判断，不能进入 Job Result 或普通日志。
func runCapturedCommand(ctx context.Context, executablePath string, args []string, workingDirectory string, limit int64) ([]byte, []byte, bool, error) {
	command := exec.CommandContext(ctx, executablePath, args...)
	command.Dir = workingDirectory
	command.Env = append([]string(nil), minimalProcessEnvironment...)
	stdout := newLimitedCapture(limit)
	stderr := newLimitedCapture(limit)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	return stdout.Bytes(), stderr.Bytes(), stdout.Truncated() || stderr.Truncated(), err
}

// limitedCapture 保留外部命令输出的有限前缀并无阻塞地丢弃超额字节。
type limitedCapture struct {
	// buffer 只保存未超过上限的诊断前缀。
	buffer bytes.Buffer
	// remaining 是仍可保留的字节数。
	remaining int64
	// truncated 表示至少一个字节因超过上限而被丢弃。
	truncated bool
}

// newLimitedCapture 创建具有固定容量的命令输出接收器。
func newLimitedCapture(limit int64) *limitedCapture {
	return &limitedCapture{remaining: limit}
}

// Write 实现 io.Writer；始终报告已消费完整输入，避免因诊断截断改变子进程退出语义。
func (c *limitedCapture) Write(payload []byte) (int, error) {
	originalLength := len(payload)
	if c.remaining <= 0 {
		c.truncated = c.truncated || originalLength > 0
		return originalLength, nil
	}
	keep := int64(len(payload))
	if keep > c.remaining {
		keep = c.remaining
		c.truncated = true
	}
	_, _ = c.buffer.Write(payload[:keep])
	c.remaining -= keep
	return originalLength, nil
}

// Bytes 返回已保留输出的副本，防止调用方修改接收器内部缓冲。
func (c *limitedCapture) Bytes() []byte {
	return bytes.Clone(c.buffer.Bytes())
}

// Truncated 报告外部命令输出是否超过冻结上限。
func (c *limitedCapture) Truncated() bool {
	return c.truncated
}

var _ io.Writer = (*limitedCapture)(nil)

// commandContext 把调用 Context 与 Envelope Deadline 收敛为不晚于任一边界的执行 Context。
func commandContext(parent context.Context, deadline time.Time) (context.Context, context.CancelFunc, error) {
	if parent == nil || deadline.IsZero() {
		return nil, nil, newArtifactError(ErrorCodeInvalidArgument, "build_command_context", nil)
	}
	if err := parent.Err(); err != nil {
		return nil, nil, newArtifactError(ErrorCodeExecutionTimeout, "build_command_context", err)
	}
	if !deadline.After(time.Now()) {
		return nil, nil, newArtifactError(ErrorCodeExecutionTimeout, "build_command_context", context.DeadlineExceeded)
	}
	if parentDeadline, ok := parent.Deadline(); ok && !deadline.Before(parentDeadline) {
		child, cancel := context.WithCancel(parent)
		return child, cancel, nil
	}
	child, cancel := context.WithDeadline(parent, deadline)
	return child, cancel, nil
}

// isContextTimeout 判断外部命令失败是否由调用取消或 Deadline 导致。
func isContextTimeout(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
