package mediapreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalObjectStorePromotesAndVerifiesPNGAndMP4WithoutPathAliases(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := OpenLocalObjectStore(root)
	if err != nil {
		t.Fatalf("OpenLocalObjectStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, directory := range []string{"staging", "objects"} {
		info, err := os.Lstat(filepath.Join(root, directory))
		if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
			t.Fatalf("unsafe fixed directory %s: info=%v error=%v", directory, info, err)
		}
	}

	pngStaging, pngFinal, pngOutput := writeStorePNGFixture(t, store, root)
	if err := store.Promote(pngStaging, pngFinal, pngOutput); err != nil {
		t.Fatalf("Promote(PNG) error = %v", err)
	}
	file, err := store.OpenVerified(pngFinal, pngOutput)
	if err != nil {
		t.Fatalf("OpenVerified(PNG) error = %v", err)
	}
	if data, err := io.ReadAll(file); err != nil || int64(len(data)) != pngOutput.SizeBytes {
		t.Fatalf("read verified PNG size=%d error=%v", len(data), err)
	}
	_ = file.Close()
	if err := store.Promote(pngStaging, pngFinal, pngOutput); err != nil {
		t.Fatalf("same published PNG did not converge: %v", err)
	}

	mp4AssetID := mediaPreviewTestUUIDv7(t)
	mp4PreparationID := mediaPreviewTestUUIDv7(t)
	mp4Staging, mp4Final, _ := ObjectKeys(mp4AssetID, mp4PreparationID, ToolAssembleOutput)
	if err := store.EnsurePreparation(mp4Staging, mp4Final); err != nil {
		t.Fatal(err)
	}
	mp4Bytes := minimalFastStartMP4()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(mp4Staging)), mp4Bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	mp4Output := OutputMetadata{
		ContentDigest: sha256.Sum256(mp4Bytes), SizeBytes: int64(len(mp4Bytes)), MIMEType: MIMEMP4,
		Width: PNGWidth, Height: PNGHeight, DurationMS: MP4DurationMS, Codec: "h264", PixelFormat: "yuv420p",
	}
	if err := store.Promote(mp4Staging, mp4Final, mp4Output); err != nil {
		t.Fatalf("Promote(MP4) error = %v", err)
	}

	hardlinkAssetID := mediaPreviewTestUUIDv7(t)
	hardlinkPreparationID := mediaPreviewTestUUIDv7(t)
	hardlinkStaging, hardlinkFinal, _ := ObjectKeys(hardlinkAssetID, hardlinkPreparationID, ToolGenerateMedia)
	if err := store.EnsurePreparation(hardlinkStaging, hardlinkFinal); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(root, filepath.FromSlash(hardlinkStaging))
	if err := os.WriteFile(original, bytes.Repeat([]byte{1}, 16), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(original, original+".alias"); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(hardlinkStaging, pngOutput); !errors.Is(err, ErrArtifactInvalid) {
		t.Fatalf("hardlink artifact error=%v", err)
	}
	if err := store.EnsurePreparation("staging/../objects/escape", hardlinkFinal); err == nil {
		t.Fatal("traversal key was accepted")
	}
}

func TestOpenLocalObjectStoreRejectsSymlinkAndWideRoot(t *testing.T) {
	realRoot := t.TempDir()
	if err := os.Chmod(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkRoot := filepath.Join(t.TempDir(), "root-link")
	if err := os.Symlink(realRoot, symlinkRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLocalObjectStore(symlinkRoot); !errors.Is(err, ErrDependencyNotReady) {
		t.Fatalf("symlink root error=%v", err)
	}
	if err := os.Chmod(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenLocalObjectStore(realRoot); !errors.Is(err, ErrDependencyNotReady) {
		t.Fatalf("wide root error=%v", err)
	}
}

func writeStorePNGFixture(t *testing.T, store *LocalObjectStore, root string) (string, string, OutputMetadata) {
	t.Helper()
	assetID := mediaPreviewTestUUIDv7(t)
	preparationID := mediaPreviewTestUUIDv7(t)
	staging, final, _ := ObjectKeys(assetID, preparationID, ToolGenerateMedia)
	if err := store.EnsurePreparation(staging, final); err != nil {
		t.Fatal(err)
	}
	preview := image.NewRGBA(image.Rect(0, 0, PNGWidth, PNGHeight))
	for y := 0; y < PNGHeight; y++ {
		for x := 0; x < PNGWidth; x++ {
			preview.SetRGBA(x, y, color.RGBA{R: byte(x), G: byte(y), B: 0x7f, A: 0xff})
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, preview); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(staging)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return staging, final, OutputMetadata{
		ContentDigest: sha256.Sum256(data), SizeBytes: int64(len(data)), MIMEType: MIMEPNG,
		Width: PNGWidth, Height: PNGHeight,
	}
}

func minimalFastStartMP4() []byte {
	box := func(kind string, payload []byte) []byte {
		result := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(result[:4], uint32(len(result)))
		copy(result[4:8], kind)
		copy(result[8:], payload)
		return result
	}
	return bytes.Join([][]byte{
		box("ftyp", []byte{'i', 's', 'o', 'm', 0, 0, 0, 1}),
		box("moov", nil),
		box("mdat", nil),
	}, nil)
}
