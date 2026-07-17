package mediapreview

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"
)

const fixedVideoFilter = "scale=640:360:force_original_aspect_ratio=decrease,pad=640:360:(ow-iw)/2:(oh-ih)/2:color=black"

// AssembleMP4 重新解码安全源 PNG，并用契约冻结的 ffmpeg argv 生成 640x360 H.264 faststart MP4。
//
// 方法不接受 codec、filter、fps、duration 或任意附加参数；输出经 Sync/Close、真实 ffprobe、box 顺序、
// SHA/Size 验证后才原子发布并返回回执。
func (e *Engine) AssembleMP4(ctx context.Context, envelope MediaJobEnvelopeV1) (ArtifactReceiptV1, error) {
	if e == nil || e.store == nil || e.profile != RuntimeProfileMediaV3Preview1 || e.ffmpegPath == "" || e.ffprobePath == "" {
		return ArtifactReceiptV1{}, newArtifactError(ErrorCodeFFmpegUnavailable, "assemble_mp4", nil)
	}
	if err := envelope.Validate(); err != nil {
		return ArtifactReceiptV1{}, err
	}
	if envelope.JobType != JobTypeAssembleMP4 {
		return ArtifactReceiptV1{}, newArtifactError(ErrorCodeUnsupportedProfile, "assemble_mp4", nil)
	}
	executionContext, cancel, err := commandContext(ctx, envelope.DeadlineAt)
	if err != nil {
		return ArtifactReceiptV1{}, err
	}
	defer cancel()
	if err := e.acquireMediaSlot(executionContext); err != nil {
		return ArtifactReceiptV1{}, err
	}
	defer e.releaseMediaSlot()

	sourceKey := envelope.SourceRef.SourceObjectKey
	if err := e.verifyPNG(sourceKey); err != nil {
		return ArtifactReceiptV1{}, err
	}
	sourcePath, err := e.store.absolutePath(sourceKey)
	if err != nil {
		return ArtifactReceiptV1{}, err
	}

	targetKey := envelope.Target.StagingObjectKey
	partKey := targetKey + "." + envelope.AttemptID.String() + "." + strconv.FormatInt(envelope.Fence, 10) + ".part.mp4"
	if err := e.store.inspectOutput(targetKey); err != nil {
		return ArtifactReceiptV1{}, err
	}
	partFile, err := e.store.createPart(partKey)
	if err != nil {
		return ArtifactReceiptV1{}, err
	}
	if err := partFile.Close(); err != nil {
		e.store.removePart(partKey)
		return ArtifactReceiptV1{}, newArtifactError(ErrorCodeInternal, "prepare_mp4_part", err)
	}
	partCreated := true
	defer func() {
		if partCreated {
			e.store.removePart(partKey)
		}
	}()
	partPath, err := e.store.absolutePath(partKey)
	if err != nil {
		return ArtifactReceiptV1{}, err
	}

	_, _, truncated, commandErr := runCapturedCommand(
		executionContext,
		e.ffmpegPath,
		fixedFFmpegArgs(sourcePath, partPath),
		e.store.rootPath,
		e.stderrLimit,
	)
	if commandErr != nil {
		if isContextTimeout(executionContext, commandErr) {
			return ArtifactReceiptV1{}, newArtifactError(ErrorCodeExecutionTimeout, "run_ffmpeg", commandErr)
		}
		return ArtifactReceiptV1{}, newArtifactError(ErrorCodeArtifactInvalid, "run_ffmpeg", commandErr)
	}
	if truncated {
		return ArtifactReceiptV1{}, newArtifactError(ErrorCodeArtifactInvalid, "run_ffmpeg", nil)
	}
	if err := e.syncMP4Part(partKey); err != nil {
		return ArtifactReceiptV1{}, err
	}

	probe, err := e.probeMP4(executionContext, partKey)
	if err != nil {
		return ArtifactReceiptV1{}, err
	}
	if err := e.verifyFastStart(partKey); err != nil {
		return ArtifactReceiptV1{}, err
	}
	digest, size, err := e.digestAndSize(partKey)
	if err != nil {
		return ArtifactReceiptV1{}, err
	}
	if err := e.store.publishPart(partKey, targetKey); err != nil {
		return ArtifactReceiptV1{}, err
	}
	partCreated = false

	return ArtifactReceiptV1{
		SchemaVersion:         ArtifactReceiptSchemaVersionV1,
		JobID:                 envelope.JobID,
		AttemptID:             envelope.AttemptID,
		Fence:                 envelope.Fence,
		JobType:               envelope.JobType,
		ArtifactRequestDigest: envelope.ArtifactRequestDigest,
		ObjectKey:             targetKey,
		ContentDigest:         digest,
		SizeBytes:             size,
		MIMEType:              "video/mp4",
		Width:                 probe.Width,
		Height:                probe.Height,
		DurationMS:            probe.DurationMS,
		Codec:                 probe.Codec,
		PixelFormat:           probe.PixelFormat,
	}, nil
}

// fixedFFmpegArgs 构建设计文档冻结的完整 argv；两个绝对路径是唯一动态值。
func fixedFFmpegArgs(sourcePath string, outputPartPath string) []string {
	return []string{
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-loop", "1",
		"-framerate", "30",
		"-i", sourcePath,
		"-t", "2.000",
		"-vf", fixedVideoFilter,
		"-r", "30",
		"-an",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		outputPartPath,
	}
}

// syncMP4Part 复核 ffmpeg 没有把预创建文件替换为链接或特殊文件，再执行 Sync/Close。
func (e *Engine) syncMP4Part(key string) error {
	file, err := e.store.openRegularReadWrite(key)
	if err != nil {
		return err
	}
	if err := syncAndClose(file); err != nil {
		return newArtifactError(ErrorCodeInternal, "persist_mp4_part", err)
	}
	return nil
}

// mp4ProbeResult 是已经通过契约校验的最小 MP4 媒体元数据。
type mp4ProbeResult struct {
	// Codec 是 ffprobe 返回并校验通过的 h264 codec。
	Codec string
	// PixelFormat 是 ffprobe 返回并校验通过的 yuv420p pixel format。
	PixelFormat string
	// Width 是 ffprobe 返回并校验通过的 640 像素宽度。
	Width int
	// Height 是 ffprobe 返回并校验通过的 360 像素高度。
	Height int
	// DurationMS 是 ffprobe 时长转换后的毫秒值，必须位于 2000±100。
	DurationMS int64
}

// ffprobeDocument 是固定 show_entries 输出的进程内解码结构，不跨 Module 暴露。
type ffprobeDocument struct {
	// Streams 只接收固定 `v:0` 探针返回的一个视频流。
	Streams []struct {
		// CodecName 是 ffprobe 的 codec_name 字段。
		CodecName string `json:"codec_name"`
		// PixelFormat 是 ffprobe 的 pix_fmt 字段。
		PixelFormat string `json:"pix_fmt"`
		// Width 是 ffprobe 的 width 字段。
		Width int `json:"width"`
		// Height 是 ffprobe 的 height 字段。
		Height int `json:"height"`
	} `json:"streams"`
	// Format 接收固定 show_entries 返回的容器级时长。
	Format struct {
		// Duration 是 ffprobe 输出的秒数字符串，解析后不使用浮点比较。
		Duration string `json:"duration"`
	} `json:"format"`
}

// probeMP4 用固定 ffprobe argv 校验 H.264、yuv420p、640x360 和 2 秒时长容差。
func (e *Engine) probeMP4(ctx context.Context, key string) (mp4ProbeResult, error) {
	absolutePath, err := e.store.absolutePath(key)
	if err != nil {
		return mp4ProbeResult{}, err
	}
	stdout, _, truncated, commandErr := runCapturedCommand(
		ctx,
		e.ffprobePath,
		[]string{
			"-v", "error",
			"-select_streams", "v:0",
			"-show_entries", "stream=codec_name,pix_fmt,width,height:format=duration",
			"-of", "json",
			absolutePath,
		},
		e.store.rootPath,
		e.stderrLimit,
	)
	if commandErr != nil {
		if isContextTimeout(ctx, commandErr) {
			return mp4ProbeResult{}, newArtifactError(ErrorCodeExecutionTimeout, "run_ffprobe", commandErr)
		}
		return mp4ProbeResult{}, newArtifactError(ErrorCodeArtifactInvalid, "run_ffprobe", commandErr)
	}
	if truncated {
		return mp4ProbeResult{}, newArtifactError(ErrorCodeArtifactInvalid, "run_ffprobe", nil)
	}

	var document ffprobeDocument
	if err := json.Unmarshal(stdout, &document); err != nil || len(document.Streams) != 1 {
		return mp4ProbeResult{}, newArtifactError(ErrorCodeArtifactInvalid, "parse_ffprobe", err)
	}
	duration, err := time.ParseDuration(strings.TrimSpace(document.Format.Duration) + "s")
	if err != nil {
		return mp4ProbeResult{}, newArtifactError(ErrorCodeArtifactInvalid, "parse_ffprobe", err)
	}
	stream := document.Streams[0]
	durationMS := duration.Milliseconds()
	if stream.CodecName != "h264" || stream.PixelFormat != "yuv420p" ||
		stream.Width != PNGWidth || stream.Height != PNGHeight ||
		durationMS < MP4DurationMS-100 || durationMS > MP4DurationMS+100 {
		return mp4ProbeResult{}, newArtifactError(ErrorCodeArtifactInvalid, "validate_ffprobe", nil)
	}
	return mp4ProbeResult{
		Codec:       stream.CodecName,
		PixelFormat: stream.PixelFormat,
		Width:       stream.Width,
		Height:      stream.Height,
		DurationMS:  durationMS,
	}, nil
}

// verifyFastStart 解析 MP4 顶层 box，要求存在 ftyp、moov、mdat 且 moov 位于第一个 mdat 前。
func (e *Engine) verifyFastStart(key string) error {
	file, err := e.store.openRegular(key)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return newArtifactError(ErrorCodeArtifactInvalid, "verify_mp4_boxes", err)
	}
	validationErr := validateTopLevelMP4Boxes(file, info.Size())
	closeErr := file.Close()
	if validationErr != nil || closeErr != nil {
		return newArtifactError(ErrorCodeArtifactInvalid, "verify_mp4_boxes", errors.Join(validationErr, closeErr))
	}
	return nil
}

// validateTopLevelMP4Boxes 对 ISO BMFF 顶层 box 做有界顺序解析，不读取 mdat 内容到内存。
func validateTopLevelMP4Boxes(reader io.ReaderAt, fileSize int64) error {
	if fileSize < 8 {
		return errors.New("mp4 is smaller than one box header")
	}
	var offset int64
	var ftypOffset int64 = -1
	var moovOffset int64 = -1
	var mdatOffset int64 = -1
	header := make([]byte, 16)
	for offset < fileSize {
		if _, err := reader.ReadAt(header[:8], offset); err != nil {
			return err
		}
		boxSize := int64(binary.BigEndian.Uint32(header[:4]))
		headerSize := int64(8)
		if boxSize == 1 {
			if _, err := reader.ReadAt(header[8:16], offset+8); err != nil {
				return err
			}
			boxSize = int64(binary.BigEndian.Uint64(header[8:16]))
			headerSize = 16
		} else if boxSize == 0 {
			boxSize = fileSize - offset
		}
		if boxSize < headerSize || boxSize > fileSize-offset {
			return errors.New("invalid mp4 box size")
		}
		switch string(header[4:8]) {
		case "ftyp":
			if ftypOffset < 0 {
				ftypOffset = offset
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
	if offset != fileSize || ftypOffset < 0 || moovOffset < 0 || mdatOffset < 0 || moovOffset >= mdatOffset {
		return errors.New("mp4 is not faststart")
	}
	return nil
}
