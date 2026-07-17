package mediapreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strconv"
)

// GeneratePNG 使用 scope_digest、target_digest 和 generator_version 派生固定像素并发布真实 PNG。
//
// 方法只写 Business 指定 staging key；写入先落到 0600 的 *.part，Sync/Close、完整 Decode、SHA/Size
// 验证通过后才原子 rename。任何失败都不会返回可供 Finalize ready 的回执。
func (e *Engine) GeneratePNG(ctx context.Context, envelope MediaJobEnvelopeV1) (ArtifactReceiptV1, error) {
	if e == nil || e.store == nil || e.profile != RuntimeProfileMediaV3Preview1 {
		return ArtifactReceiptV1{}, newArtifactError(ErrorCodeUnsupportedProfile, "generate_png", nil)
	}
	if err := envelope.Validate(); err != nil {
		return ArtifactReceiptV1{}, err
	}
	if envelope.JobType != JobTypeGeneratePNG {
		return ArtifactReceiptV1{}, newArtifactError(ErrorCodeUnsupportedProfile, "generate_png", nil)
	}
	executionContext, cancel, err := commandContext(ctx, envelope.DeadlineAt)
	if err != nil {
		return ArtifactReceiptV1{}, err
	}
	defer cancel()

	targetKey := envelope.Target.StagingObjectKey
	partKey := targetKey + "." + envelope.AttemptID.String() + "." + strconv.FormatInt(envelope.Fence, 10) + ".part"
	if err := e.store.inspectOutput(targetKey); err != nil {
		return ArtifactReceiptV1{}, err
	}
	partFile, err := e.store.createPart(partKey)
	if err != nil {
		return ArtifactReceiptV1{}, err
	}
	partCreated := true
	defer func() {
		if partCreated {
			e.store.removePart(partKey)
		}
	}()

	if err := executionContext.Err(); err != nil {
		_ = partFile.Close()
		return ArtifactReceiptV1{}, newArtifactError(ErrorCodeExecutionTimeout, "generate_png", err)
	}
	previewImage := deterministicPNGImage(envelope.ScopeDigest, envelope.SourceRef.TargetDigest, e.generatorVersion)
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(partFile, previewImage); err != nil {
		_ = partFile.Close()
		return ArtifactReceiptV1{}, newArtifactError(ErrorCodeInternal, "encode_png", err)
	}
	if err := syncAndClose(partFile); err != nil {
		return ArtifactReceiptV1{}, newArtifactError(ErrorCodeInternal, "persist_png_part", err)
	}
	if err := executionContext.Err(); err != nil {
		return ArtifactReceiptV1{}, newArtifactError(ErrorCodeExecutionTimeout, "generate_png", err)
	}
	if err := e.verifyPNG(partKey); err != nil {
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
		GeneratorVersion:      e.generatorVersion,
		ArtifactRequestDigest: envelope.ArtifactRequestDigest,
		ObjectKey:             targetKey,
		ContentDigest:         digest,
		SizeBytes:             size,
		MIMEType:              "image/png",
		Width:                 PNGWidth,
		Height:                PNGHeight,
	}, nil
}

// deterministicPNGImage 根据冻结的三个字符串直接派生每个 RGBA 像素，不使用时间、随机数、字体或网络。
func deterministicPNGImage(scopeDigest string, targetDigest string, generatorVersion GeneratorVersion) *image.RGBA {
	seed := sha256.Sum256([]byte(scopeDigest + targetDigest + string(generatorVersion)))
	previewImage := image.NewRGBA(image.Rect(0, 0, PNGWidth, PNGHeight))
	for y := 0; y < PNGHeight; y++ {
		for x := 0; x < PNGWidth; x++ {
			red := seed[(x+y)%len(seed)] ^ byte(x)
			green := seed[(x*3+y*5)%len(seed)] ^ byte(y)
			blue := seed[(x*7+y*11)%len(seed)] ^ byte(x+y)
			previewImage.SetRGBA(x, y, color.RGBA{R: red, G: green, B: blue, A: 0xff})
		}
	}
	return previewImage
}

// verifyPNG 重新打开 part，先 DecodeConfig 再完整 Decode，并严格验证 640x360 尺寸。
func (e *Engine) verifyPNG(key string) error {
	file, err := e.store.openRegular(key)
	if err != nil {
		return err
	}
	configuration, err := png.DecodeConfig(file)
	if err != nil || configuration.Width != PNGWidth || configuration.Height != PNGHeight {
		_ = file.Close()
		return newArtifactError(ErrorCodeArtifactInvalid, "verify_png", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return newArtifactError(ErrorCodeArtifactInvalid, "verify_png", err)
	}
	decoded, err := png.Decode(file)
	closeErr := file.Close()
	if err != nil || closeErr != nil || decoded.Bounds().Dx() != PNGWidth || decoded.Bounds().Dy() != PNGHeight {
		return newArtifactError(ErrorCodeArtifactInvalid, "verify_png", errors.Join(err, closeErr))
	}
	return nil
}

// digestAndSize 对已经通过 inode 安全校验的文件流式计算 lowercase SHA-256 和精确 Size。
func (e *Engine) digestAndSize(key string) (string, int64, error) {
	file, err := e.store.openRegular(key)
	if err != nil {
		return "", 0, err
	}
	hash := sha256.New()
	size, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return "", 0, newArtifactError(ErrorCodeInternal, "digest_artifact", errors.Join(copyErr, closeErr))
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

// syncAndClose 在返回前先持久化文件内容再关闭句柄，任一失败都阻止产物发布。
func syncAndClose(file *os.File) error {
	if file == nil {
		return errors.New("nil artifact file")
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	return errors.Join(syncErr, closeErr)
}
