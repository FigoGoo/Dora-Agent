package asset

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// LocalUploader persists assets below a configured directory and returns an
// HTTP URL served by the local AIGC router. It is intentionally limited to the
// trusted local demo and is not a replacement for private production storage.
type LocalUploader struct {
	rootDir string
	baseURL string
}

func NewLocalUploader(rootDir, baseURL string) (*LocalUploader, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("local asset root directory is required")
	}
	absolute, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve local asset root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o755); err != nil {
		return nil, fmt.Errorf("create local asset root: %w", err)
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "/api/aigc/local-assets"
	}
	return &LocalUploader{rootDir: absolute, baseURL: baseURL}, nil
}

func (u *LocalUploader) RootDir() string {
	if u == nil {
		return ""
	}
	return u.rootDir
}

func (u *LocalUploader) Upload(ctx context.Context, input UploadInput) (UploadResult, error) {
	if u == nil || strings.TrimSpace(u.rootDir) == "" {
		return UploadResult{}, fmt.Errorf("local uploader is not configured")
	}
	select {
	case <-ctx.Done():
		return UploadResult{}, ctx.Err()
	default:
	}
	objectKey, err := safeLocalObjectKey(input.ObjectKey)
	if err != nil {
		return UploadResult{}, err
	}
	if input.Content == nil {
		return UploadResult{}, fmt.Errorf("upload content is required")
	}
	destination := filepath.Join(u.rootDir, filepath.FromSlash(objectKey))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return UploadResult{}, fmt.Errorf("create local asset directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), ".upload-*")
	if err != nil {
		return UploadResult{}, fmt.Errorf("create local asset temp file: %w", err)
	}
	temporaryName := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryName)
		}
	}()
	written, err := copyWithContext(ctx, temporary, input.Content)
	if err != nil {
		return UploadResult{}, fmt.Errorf("write local asset %s: %w", objectKey, err)
	}
	if input.ContentLength >= 0 && input.ContentLength != written {
		return UploadResult{}, fmt.Errorf("local asset %s length mismatch: wrote %d, expected %d", objectKey, written, input.ContentLength)
	}
	if err := temporary.Sync(); err != nil {
		return UploadResult{}, fmt.Errorf("sync local asset %s: %w", objectKey, err)
	}
	if err := temporary.Close(); err != nil {
		return UploadResult{}, fmt.Errorf("close local asset %s: %w", objectKey, err)
	}
	if err := os.Rename(temporaryName, destination); err != nil {
		return UploadResult{}, fmt.Errorf("commit local asset %s: %w", objectKey, err)
	}
	committed = true
	return UploadResult{
		Provider: StorageProviderLocal, Bucket: "local", ObjectKey: objectKey,
		URL: localAssetURL(u.baseURL, objectKey), SizeBytes: written,
	}, nil
}

func safeLocalObjectKey(value string) (string, error) {
	value = strings.TrimLeft(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")), "/")
	cleaned := path.Clean(value)
	if value == "" || cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("invalid local asset object key %q", value)
	}
	return cleaned, nil
}

func localAssetURL(baseURL, objectKey string) string {
	segments := strings.Split(objectKey, "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	return strings.TrimRight(baseURL, "/") + "/" + strings.Join(segments, "/")
}

func copyWithContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 32*1024)
	var total int64
	for {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			written, writeErr := destination.Write(buffer[:read])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != read {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return total, nil
			}
			return total, readErr
		}
	}
}
