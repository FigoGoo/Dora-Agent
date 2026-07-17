package mediapreview

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateObjectKeyRejectsUnsafeValues(t *testing.T) {
	t.Parallel()
	testCases := []string{
		"",
		"/absolute/file.png",
		"../escape.png",
		"objects/../escape.png",
		"objects\\escape.png",
		"objects/./file.png",
		"objects//file.png",
		"objects/file.png/",
		"objects/\x00file.png",
		"objects/new\nline.png",
	}
	for _, key := range testCases {
		key := key
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			if err := validateObjectKey(key); err == nil || CodeOf(err) != ErrorCodeInvalidArgument {
				t.Fatalf("key %q should be rejected, got %v", key, err)
			}
		})
	}
	if err := validateObjectKey("staging/objects/valid-file.png"); err != nil {
		t.Fatalf("valid relative key rejected: %v", err)
	}
}

func TestNewObjectStoreRejectsUnsafeRoot(t *testing.T) {
	t.Parallel()
	if _, err := newObjectStore("relative/root"); err == nil {
		t.Fatal("relative root should be rejected")
	}

	wideRoot := t.TempDir()
	if err := os.Chmod(wideRoot, 0o755); err != nil {
		t.Fatalf("chmod wide root: %v", err)
	}
	if _, err := newObjectStore(wideRoot); err == nil {
		t.Fatal("0755 root should be rejected")
	}

	targetRoot := t.TempDir()
	symlinkParent := t.TempDir()
	symlinkRoot := filepath.Join(symlinkParent, "object-root")
	if err := os.Symlink(targetRoot, symlinkRoot); err != nil {
		t.Fatalf("create root symlink: %v", err)
	}
	if _, err := newObjectStore(symlinkRoot); err == nil {
		t.Fatal("symlink root should be rejected")
	}
}

func TestObjectStoreRejectsSymlinkHardlinkAndNonRegularSource(t *testing.T) {
	t.Parallel()
	root := newTestObjectRoot(t)
	store, err := newObjectStore(root)
	if err != nil {
		t.Fatalf("create object store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	regularPath := filepath.Join(root, "objects", "source.png")
	if err := os.WriteFile(regularPath, []byte("not-a-png"), 0o600); err != nil {
		t.Fatalf("create regular source: %v", err)
	}
	file, err := store.openRegular("objects/source.png")
	if err != nil {
		t.Fatalf("open regular source: %v", err)
	}
	_ = file.Close()

	symlinkPath := filepath.Join(root, "objects", "source-link.png")
	if err := os.Symlink("source.png", symlinkPath); err != nil {
		t.Fatalf("create source symlink: %v", err)
	}
	if _, err := store.openRegular("objects/source-link.png"); err == nil || CodeOf(err) != ErrorCodeArtifactInvalid {
		t.Fatalf("source symlink should be rejected, got %v", err)
	}

	if _, err := store.openRegular("objects"); err == nil || CodeOf(err) != ErrorCodeArtifactInvalid {
		t.Fatalf("source directory should be rejected, got %v", err)
	}

	hardlinkPath := filepath.Join(root, "objects", "source-hardlink.png")
	if err := os.Link(regularPath, hardlinkPath); err != nil {
		t.Fatalf("create source hardlink: %v", err)
	}
	if _, err := store.openRegular("objects/source.png"); err == nil || CodeOf(err) != ErrorCodeArtifactInvalid {
		t.Fatalf("multiply-linked source should be rejected, got %v", err)
	}
}

func TestObjectStoreRejectsSymlinkParent(t *testing.T) {
	t.Parallel()
	root := newTestObjectRoot(t)
	store, err := newObjectStore(root)
	if err != nil {
		t.Fatalf("create object store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	if err := os.Symlink("objects", filepath.Join(root, "linked-objects")); err != nil {
		t.Fatalf("create parent symlink: %v", err)
	}
	if _, err := store.createPart("linked-objects/output.part"); err == nil || CodeOf(err) != ErrorCodeInvalidArgument {
		t.Fatalf("symlink parent should be rejected, got %v", err)
	}
}
