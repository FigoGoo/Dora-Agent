package mediapreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGeneratePNGIsDeterministicAndDecodable(t *testing.T) {
	t.Parallel()
	root := newTestObjectRoot(t)
	engine := newPNGOnlyEngine(t, root)
	envelope := validGenerateEnvelope(t, "staging/objects/output.png")

	firstReceipt, err := engine.GeneratePNG(t.Context(), envelope)
	if err != nil {
		t.Fatalf("generate first PNG: %v", err)
	}
	firstBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(firstReceipt.ObjectKey)))
	if err != nil {
		t.Fatalf("read first PNG: %v", err)
	}
	configuration, err := png.DecodeConfig(bytes.NewReader(firstBytes))
	if err != nil {
		t.Fatalf("decode generated PNG: %v", err)
	}
	if configuration.Width != PNGWidth || configuration.Height != PNGHeight {
		t.Fatalf("unexpected PNG dimensions: %dx%d", configuration.Width, configuration.Height)
	}
	if _, err := png.Decode(bytes.NewReader(firstBytes)); err != nil {
		t.Fatalf("fully decode generated PNG: %v", err)
	}
	digest := sha256.Sum256(firstBytes)
	if firstReceipt.ContentDigest != hex.EncodeToString(digest[:]) || firstReceipt.SizeBytes != int64(len(firstBytes)) {
		t.Fatalf("receipt digest/size do not match bytes: %+v", firstReceipt)
	}
	if firstReceipt.MIMEType != "image/png" || firstReceipt.Width != PNGWidth || firstReceipt.Height != PNGHeight {
		t.Fatalf("unexpected PNG receipt metadata: %+v", firstReceipt)
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(firstReceipt.ObjectKey)))
	if err != nil {
		t.Fatalf("stat generated PNG: %v", err)
	}
	if info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() {
		t.Fatalf("generated PNG must be a 0600 regular file, got %v", info.Mode())
	}

	secondReceipt, err := engine.GeneratePNG(t.Context(), envelope)
	if err != nil {
		t.Fatalf("generate repeated PNG: %v", err)
	}
	secondBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(secondReceipt.ObjectKey)))
	if err != nil {
		t.Fatalf("read repeated PNG: %v", err)
	}
	if !bytes.Equal(firstBytes, secondBytes) || firstReceipt.ContentDigest != secondReceipt.ContentDigest {
		t.Fatal("identical scope/target/generator inputs must be byte-for-byte deterministic")
	}
	partName := "output.png." + envelope.AttemptID.String() + "." + strconv.FormatInt(envelope.Fence, 10) + ".part"
	if _, err := os.Stat(filepath.Join(root, "staging", "objects", partName)); !os.IsNotExist(err) {
		t.Fatalf("published PNG must not leave .part file, got %v", err)
	}

	changedEnvelope := validGenerateEnvelope(t, "staging/objects/changed.png")
	changedEnvelope.ScopeDigest = envelope.ScopeDigest
	changedEnvelope.SourceRef.TargetDigest = strings.Repeat("a", 64)
	changedReceipt, err := engine.GeneratePNG(t.Context(), changedEnvelope)
	if err != nil {
		t.Fatalf("generate changed PNG: %v", err)
	}
	changedBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(changedReceipt.ObjectKey)))
	if err != nil {
		t.Fatalf("read changed PNG: %v", err)
	}
	if bytes.Equal(firstBytes, changedBytes) {
		t.Fatal("changing target_digest must change deterministic PNG bytes")
	}
}

func TestGeneratePNGTakeoverDoesNotReuseOldAttemptPart(t *testing.T) {
	t.Parallel()
	root := newTestObjectRoot(t)
	engine := newPNGOnlyEngine(t, root)
	envelope := validGenerateEnvelope(t, "staging/objects/takeover.png")
	stalePart := filepath.Join(root, "staging", "objects", "takeover.png."+mustUUIDv7(t).String()+".1.part")
	if err := os.WriteFile(stalePart, []byte("old worker partial bytes"), 0o600); err != nil {
		t.Fatalf("create stale old Attempt part: %v", err)
	}
	if _, err := engine.GeneratePNG(t.Context(), envelope); err != nil {
		t.Fatalf("new Fence should ignore old Attempt part: %v", err)
	}
	if _, err := os.Stat(stalePart); err != nil {
		t.Fatalf("new Attempt must not delete another Attempt part: %v", err)
	}
}

func TestGeneratePNGRejectsUnsafeTargetBeforeWrite(t *testing.T) {
	t.Parallel()
	root := newTestObjectRoot(t)
	engine := newPNGOnlyEngine(t, root)
	envelope := validGenerateEnvelope(t, "../escaped.png")

	if _, err := engine.GeneratePNG(t.Context(), envelope); err == nil || CodeOf(err) != ErrorCodeInvalidArgument {
		t.Fatalf("unsafe output key should be rejected, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "..", "escaped.png")); err == nil {
		t.Fatal("unsafe output key wrote outside object root")
	}
}
