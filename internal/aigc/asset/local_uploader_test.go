package asset

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalUploaderPersistsAndReturnsServableURL(t *testing.T) {
	root := t.TempDir()
	uploader, err := NewLocalUploader(root, "/api/aigc/local-assets")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("local-demo")
	result, err := uploader.Upload(context.Background(), UploadInput{
		ObjectKey: "aigc/sessions/s1/assets/a1/demo file.txt", Content: bytes.NewReader(payload), ContentLength: int64(len(payload)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Provider != StorageProviderLocal || result.URL != "/api/aigc/local-assets/aigc/sessions/s1/assets/a1/demo%20file.txt" {
		t.Fatalf("upload result = %+v", result)
	}
	stored, err := os.ReadFile(filepath.Join(root, "aigc", "sessions", "s1", "assets", "a1", "demo file.txt"))
	if err != nil || !bytes.Equal(stored, payload) {
		t.Fatalf("stored payload = %q err=%v", stored, err)
	}
}

func TestLocalUploaderRejectsTraversalAndLengthMismatch(t *testing.T) {
	uploader, err := NewLocalUploader(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uploader.Upload(context.Background(), UploadInput{ObjectKey: "../secret", Content: bytes.NewReader([]byte("x")), ContentLength: 1}); err == nil {
		t.Fatal("path traversal was accepted")
	}
	if _, err := uploader.Upload(context.Background(), UploadInput{ObjectKey: "safe/file", Content: bytes.NewReader([]byte("x")), ContentLength: 2}); err == nil {
		t.Fatal("length mismatch was accepted")
	}
}
