package asset

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos/enum"

	aigcconfig "github.com/FigoGoo/Dora-Agent/internal/aigc/config"
)

type UploadInput struct {
	ObjectKey     string
	Content       io.Reader
	ContentLength int64
	MIMEType      string
	Filename      string
	Metadata      map[string]string
}

type UploadResult struct {
	Provider  string
	Bucket    string
	ObjectKey string
	URL       string
	SizeBytes int64
}

type Uploader interface {
	Upload(ctx context.Context, input UploadInput) (UploadResult, error)
}

type tosClient interface {
	PutObjectV2(ctx context.Context, input *tos.PutObjectV2Input) (*tos.PutObjectV2Output, error)
}

type TOSUploader struct {
	cfg    aigcconfig.TOSConfig
	client tosClient
}

func NewTOSUploader(cfg aigcconfig.TOSConfig) (*TOSUploader, error) {
	cfg = normalizeTOSConfig(cfg)
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("tos endpoint is required")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("tos bucket is required")
	}
	if cfg.AccessKeyID == "" || cfg.SecretAccessKey == "" {
		return nil, fmt.Errorf("tos credentials are required")
	}
	options := []tos.ClientOption{
		tos.WithCredentials(tos.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey)),
		tos.WithRequestTimeout(cfg.RequestTimeout),
		tos.WithConnectionTimeout(cfg.ConnectTimeout),
	}
	if cfg.Region != "" {
		options = append(options, tos.WithRegion(cfg.Region))
	}
	client, err := tos.NewClientV2(cfg.Endpoint, options...)
	if err != nil {
		return nil, fmt.Errorf("create tos client: %w", err)
	}
	return &TOSUploader{cfg: cfg, client: client}, nil
}

func (u *TOSUploader) Upload(ctx context.Context, input UploadInput) (UploadResult, error) {
	if u == nil || u.client == nil {
		return UploadResult{}, fmt.Errorf("tos uploader is not configured")
	}
	input.ObjectKey = strings.TrimLeft(strings.TrimSpace(input.ObjectKey), "/")
	if input.ObjectKey == "" {
		return UploadResult{}, fmt.Errorf("object key is required")
	}
	if input.Content == nil {
		return UploadResult{}, fmt.Errorf("upload content is required")
	}
	if strings.TrimSpace(input.MIMEType) == "" {
		input.MIMEType = "application/octet-stream"
	}
	_, err := u.client.PutObjectV2(ctx, &tos.PutObjectV2Input{
		PutObjectBasicInput: tos.PutObjectBasicInput{
			Bucket:        u.cfg.Bucket,
			Key:           input.ObjectKey,
			ContentLength: input.ContentLength,
			ContentType:   input.MIMEType,
			ACL:           enum.ACLPublicRead,
			Meta:          input.Metadata,
		},
		Content: input.Content,
	})
	if err != nil {
		return UploadResult{}, fmt.Errorf("put tos object %s: %w", input.ObjectKey, err)
	}
	return UploadResult{
		Provider:  StorageProviderTOS,
		Bucket:    u.cfg.Bucket,
		ObjectKey: input.ObjectKey,
		URL:       BuildPublicURL(u.cfg.BaseURL, input.ObjectKey),
		SizeBytes: input.ContentLength,
	}, nil
}

func normalizeTOSConfig(cfg aigcconfig.TOSConfig) aigcconfig.TOSConfig {
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.AccessKeyID = strings.TrimSpace(cfg.AccessKeyID)
	cfg.SecretAccessKey = strings.TrimSpace(cfg.SecretAccessKey)
	cfg.Region = strings.TrimSpace(cfg.Region)
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = aigcconfig.DefaultTOSRequestTimeout
	}
	if cfg.ConnectTimeout <= 0 {
		cfg.ConnectTimeout = aigcconfig.DefaultTOSConnectTimeout
	}
	return cfg
}
