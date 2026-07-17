package mediapreview

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestFixedFFmpegArgsExactSet(t *testing.T) {
	t.Parallel()
	sourcePath := "/safe/root/objects/source.png"
	outputPath := "/safe/root/staging/output.part.mp4"
	expected := []string{
		"-nostdin",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-loop", "1",
		"-framerate", "30",
		"-i", sourcePath,
		"-t", "2.000",
		"-vf", "scale=640:360:force_original_aspect_ratio=decrease,pad=640:360:(ow-iw)/2:(oh-ih)/2:color=black",
		"-r", "30",
		"-an",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		"-movflags", "+faststart",
		outputPath,
	}
	if actual := fixedFFmpegArgs(sourcePath, outputPath); !reflect.DeepEqual(actual, expected) {
		t.Fatalf("ffmpeg argv changed\nactual: %#v\nexpected: %#v", actual, expected)
	}
}

func TestValidateExecutableRejectsRelativeSymlinkAndNonExecutable(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	executablePath := filepath.Join(directory, "ffmpeg-real")
	if err := os.WriteFile(executablePath, []byte("fixture"), 0o700); err != nil {
		t.Fatalf("create executable fixture: %v", err)
	}
	if err := validateExecutable(executablePath); err != nil {
		t.Fatalf("absolute regular executable rejected: %v", err)
	}
	if err := validateExecutable("relative/ffmpeg"); err == nil || CodeOf(err) != ErrorCodeFFmpegUnavailable {
		t.Fatalf("relative executable should be rejected, got %v", err)
	}

	nonExecutablePath := filepath.Join(directory, "ffprobe-non-executable")
	if err := os.WriteFile(nonExecutablePath, []byte("fixture"), 0o600); err != nil {
		t.Fatalf("create non-executable fixture: %v", err)
	}
	if err := validateExecutable(nonExecutablePath); err == nil || CodeOf(err) != ErrorCodeFFmpegUnavailable {
		t.Fatalf("non-executable file should be rejected, got %v", err)
	}

	symlinkPath := filepath.Join(directory, "ffmpeg-link")
	if err := os.Symlink(executablePath, symlinkPath); err != nil {
		t.Fatalf("create executable symlink: %v", err)
	}
	if err := validateExecutable(symlinkPath); err == nil || CodeOf(err) != ErrorCodeFFmpegUnavailable {
		t.Fatalf("executable symlink should be rejected, got %v", err)
	}
}

func TestValidateTopLevelMP4BoxesRequiresFastStart(t *testing.T) {
	t.Parallel()
	fastStart := appendMP4Box(nil, "ftyp", []byte("isom"))
	fastStart = appendMP4Box(fastStart, "moov", []byte("metadata"))
	fastStart = appendMP4Box(fastStart, "mdat", []byte("payload"))
	if err := validateTopLevelMP4Boxes(bytes.NewReader(fastStart), int64(len(fastStart))); err != nil {
		t.Fatalf("valid faststart box order rejected: %v", err)
	}

	nonFastStart := appendMP4Box(nil, "ftyp", []byte("isom"))
	nonFastStart = appendMP4Box(nonFastStart, "mdat", []byte("payload"))
	nonFastStart = appendMP4Box(nonFastStart, "moov", []byte("metadata"))
	if err := validateTopLevelMP4Boxes(bytes.NewReader(nonFastStart), int64(len(nonFastStart))); err == nil {
		t.Fatal("moov after mdat should be rejected")
	}
}

func TestAssembleMP4WithLocalFFmpeg(t *testing.T) {
	ffmpegPath := resolveLocalMediaExecutable(t, "ffmpeg")
	ffprobePath := resolveLocalMediaExecutable(t, "ffprobe")
	root := newTestObjectRoot(t)

	engine, err := NewEngine(t.Context(), Config{
		Profile:          RuntimeProfileMediaV3Preview1,
		ObjectRoot:       root,
		GeneratorVersion: GeneratorVersionPNG640x360V1,
		FFMPEGPath:       ffmpegPath,
		FFprobePath:      ffprobePath,
		StderrLimitBytes: DefaultStderrLimitBytes,
	})
	if err != nil {
		t.Fatalf("real ffmpeg/ffprobe readiness failed: %v", err)
	}
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Errorf("close media engine: %v", err)
		}
	})

	executionContext, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	pngEnvelope := validGenerateEnvelope(t, "objects/source.png")
	pngEnvelope.DeadlineAt = time.Now().UTC().Add(30 * time.Second)
	if _, err := engine.GeneratePNG(executionContext, pngEnvelope); err != nil {
		t.Fatalf("generate real MP4 source PNG: %v", err)
	}

	mp4Envelope := validAssembleEnvelope(t, "objects/source.png", "staging/objects/output.mp4")
	mp4Envelope.DeadlineAt = time.Now().UTC().Add(30 * time.Second)
	receipt, err := engine.AssembleMP4(executionContext, mp4Envelope)
	if err != nil {
		t.Fatalf("assemble real MP4: %v", err)
	}
	if receipt.MIMEType != "video/mp4" || receipt.Codec != "h264" || receipt.PixelFormat != "yuv420p" ||
		receipt.Width != PNGWidth || receipt.Height != PNGHeight ||
		receipt.DurationMS < MP4DurationMS-100 || receipt.DurationMS > MP4DurationMS+100 ||
		receipt.SizeBytes <= 0 || !isLowercaseSHA256(receipt.ContentDigest) {
		t.Fatalf("unexpected MP4 receipt: %+v", receipt)
	}
	outputPath := filepath.Join(root, filepath.FromSlash(receipt.ObjectKey))
	info, err := os.Stat(outputPath)
	if err != nil {
		t.Fatalf("stat real MP4: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() != receipt.SizeBytes {
		t.Fatalf("unexpected MP4 file metadata: mode=%v size=%d receipt=%+v", info.Mode(), info.Size(), receipt)
	}
	if err := engine.verifyFastStart(receipt.ObjectKey); err != nil {
		t.Fatalf("real MP4 is not faststart: %v", err)
	}
	partPath := outputPath + "." + mp4Envelope.AttemptID.String() + "." + strconv.FormatInt(mp4Envelope.Fence, 10) + ".part.mp4"
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatalf("published MP4 must not leave part file, got %v", err)
	}
}

func appendMP4Box(destination []byte, boxType string, payload []byte) []byte {
	box := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(box[:4], uint32(len(box)))
	copy(box[4:8], boxType)
	copy(box[8:], payload)
	return append(destination, box...)
}

func resolveLocalMediaExecutable(t *testing.T, name string) string {
	t.Helper()
	pathFromEnvironment, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed; real integration test is unavailable", name)
	}
	resolvedPath, err := filepath.EvalSymlinks(pathFromEnvironment)
	if err != nil {
		t.Fatalf("resolve %s executable symlinks: %v", name, err)
	}
	absolutePath, err := filepath.Abs(resolvedPath)
	if err != nil {
		t.Fatalf("resolve %s absolute path: %v", name, err)
	}
	return absolutePath
}
