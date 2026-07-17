package mediapreview

import (
	"errors"
	"strings"
	"testing"
)

func TestArtifactErrorRedactsCause(t *testing.T) {
	t.Parallel()
	sensitiveCause := errors.New("/private/object-root/secret/output.mp4: provider stderr")
	err := newArtifactError(ErrorCodeArtifactInvalid, "verify_artifact", sensitiveCause)
	if strings.Contains(err.Error(), "/private/") || strings.Contains(err.Error(), "stderr") {
		t.Fatalf("artifact error leaked underlying path or diagnostics: %q", err.Error())
	}
	if CodeOf(err) != ErrorCodeArtifactInvalid || !errors.Is(err, sensitiveCause) {
		t.Fatalf("artifact error lost stable code or in-process cause: %v", err)
	}
}

func TestLimitedCaptureCapsDiagnostics(t *testing.T) {
	t.Parallel()
	capture := newLimitedCapture(4)
	payload := []byte("sensitive-diagnostics")
	written, err := capture.Write(payload)
	if err != nil || written != len(payload) {
		t.Fatalf("limited capture changed writer semantics: written=%d err=%v", written, err)
	}
	if string(capture.Bytes()) != "sens" || !capture.Truncated() {
		t.Fatalf("limited capture did not cap diagnostics: bytes=%q truncated=%t", capture.Bytes(), capture.Truncated())
	}
}
