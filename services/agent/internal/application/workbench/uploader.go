package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/FigoGoo/Dora-Agent/services/agent/internal/runtime/modeltool"
)

type ArtifactUploader interface {
	Upload(ctx context.Context, slot GeneratedUploadSlotDTO, artifact modeltool.Artifact) (UploadedObjectDTO, error)
}

type UploadedObjectDTO struct {
	ObjectKey   string
	Bucket      string
	ContentType string
	SizeBytes   int64
	Checksum    string
	Etag        string
}

type StreamingArtifactUploader struct {
	client *http.Client
}

func NewStreamingArtifactUploader(client *http.Client) StreamingArtifactUploader {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	return StreamingArtifactUploader{client: client}
}

func (u StreamingArtifactUploader) Upload(ctx context.Context, slot GeneratedUploadSlotDTO, artifact modeltool.Artifact) (UploadedObjectDTO, error) {
	if strings.TrimSpace(slot.UploadURL) == "" || strings.TrimSpace(slot.ObjectKey) == "" || strings.TrimSpace(slot.Bucket) == "" {
		return UploadedObjectDTO{}, fmt.Errorf("upload slot is incomplete")
	}
	if artifact.SizeBytes <= 0 || strings.TrimSpace(artifact.ContentType) == "" || strings.TrimSpace(artifact.Checksum) == "" {
		return UploadedObjectDTO{}, fmt.Errorf("artifact metadata is incomplete")
	}
	stream, err := artifact.Stream(ctx)
	if err != nil {
		return UploadedObjectDTO{}, err
	}
	defer stream.Close()

	if isLocalUploadURL(slot.UploadURL) {
		return consumeLocalStream(ctx, slot, artifact, stream)
	}
	return u.putSignedURL(ctx, slot, artifact, stream)
}

func (u StreamingArtifactUploader) putSignedURL(ctx context.Context, slot GeneratedUploadSlotDTO, artifact modeltool.Artifact, stream io.Reader) (UploadedObjectDTO, error) {
	hasher := sha256.New()
	body := io.TeeReader(stream, hasher)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, slot.UploadURL, body)
	if err != nil {
		return UploadedObjectDTO{}, err
	}
	req.ContentLength = artifact.SizeBytes
	req.Header.Set("Content-Type", artifact.ContentType)
	for key, value := range slot.UploadHeaders {
		if key == "" || strings.EqualFold(key, "Authorization") {
			continue
		}
		req.Header.Set(key, value)
	}
	resp, err := u.client.Do(req)
	if err != nil {
		return UploadedObjectDTO{}, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return UploadedObjectDTO{}, fmt.Errorf("tos upload failed with status %d", resp.StatusCode)
	}
	checksum := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if checksum != artifact.Checksum {
		return UploadedObjectDTO{}, fmt.Errorf("uploaded artifact checksum mismatch")
	}
	etag := strings.Trim(resp.Header.Get("ETag"), `"`)
	if etag == "" {
		etag = "uploaded-" + checksum[len("sha256:"):len("sha256:")+16]
	}
	return UploadedObjectDTO{
		ObjectKey: slot.ObjectKey, Bucket: slot.Bucket, ContentType: artifact.ContentType,
		SizeBytes: artifact.SizeBytes, Checksum: checksum, Etag: etag,
	}, nil
}

func consumeLocalStream(ctx context.Context, slot GeneratedUploadSlotDTO, artifact modeltool.Artifact, stream io.Reader) (UploadedObjectDTO, error) {
	hasher := sha256.New()
	n, err := io.Copy(hasher, readerWithContext{ctx: ctx, r: stream})
	if err != nil {
		return UploadedObjectDTO{}, err
	}
	if n != artifact.SizeBytes {
		return UploadedObjectDTO{}, fmt.Errorf("local upload size mismatch")
	}
	checksum := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if checksum != artifact.Checksum {
		return UploadedObjectDTO{}, fmt.Errorf("local upload checksum mismatch")
	}
	return UploadedObjectDTO{
		ObjectKey: slot.ObjectKey, Bucket: slot.Bucket, ContentType: artifact.ContentType,
		SizeBytes: artifact.SizeBytes, Checksum: checksum, Etag: "uploaded-" + checksum[len("sha256:"):len("sha256:")+16],
	}, nil
}

type readerWithContext struct {
	ctx context.Context
	r   io.Reader
}

func (r readerWithContext) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func isLocalUploadURL(raw string) bool {
	return strings.HasPrefix(raw, "memory://") || strings.Contains(raw, "://localhost/tos/") || strings.Contains(raw, "://127.0.0.1/tos/")
}
