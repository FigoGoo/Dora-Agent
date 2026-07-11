package asset

import (
	"path"
	"path/filepath"
	"strings"
	"time"
)

const (
	KindImage     = "image"
	KindAudio     = "audio"
	KindVideo     = "video"
	KindPDF       = "pdf"
	KindText      = "text"
	KindReference = "reference"

	SourceUpload    = "upload"
	SourceGenerated = "generated"

	AvailabilityPendingBilling = "pending_billing"
	AvailabilityAvailable      = "available"
	AvailabilityQuarantined    = "quarantined"
	AvailabilityDeleted        = "deleted"

	StorageProviderTOS   = "tos"
	StorageProviderLocal = "local"
)

type Asset struct {
	ID              string         `json:"id"`
	SessionID       string         `json:"session_id,omitempty"`
	UserID          string         `json:"user_id,omitempty"`
	SourceJobID     string         `json:"source_job_id,omitempty"`
	OutputIndex     int            `json:"output_index,omitempty"`
	Kind            string         `json:"kind"`
	Source          string         `json:"source"`
	Availability    string         `json:"availability"`
	MIMEType        string         `json:"mime_type,omitempty"`
	Filename        string         `json:"filename,omitempty"`
	SizeBytes       int64          `json:"size_bytes,omitempty"`
	ContentHash     string         `json:"content_hash,omitempty"`
	StorageProvider string         `json:"storage_provider,omitempty"`
	Bucket          string         `json:"bucket,omitempty"`
	ObjectKey       string         `json:"object_key,omitempty"`
	URL             string         `json:"url,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at,omitempty"`
}

func NormalizeAvailability(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case AvailabilityPendingBilling:
		return AvailabilityPendingBilling
	case AvailabilityAvailable:
		return AvailabilityAvailable
	case AvailabilityQuarantined:
		return AvailabilityQuarantined
	case AvailabilityDeleted:
		return AvailabilityDeleted
	default:
		return ""
	}
}

func NormalizeKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case KindImage:
		return KindImage
	case KindAudio:
		return KindAudio
	case KindVideo:
		return KindVideo
	case KindPDF:
		return KindPDF
	case KindText:
		return KindText
	case KindReference:
		return KindReference
	default:
		return ""
	}
}

func KindFromMIME(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return KindImage
	case strings.HasPrefix(mimeType, "audio/"):
		return KindAudio
	case strings.HasPrefix(mimeType, "video/"):
		return KindVideo
	case mimeType == "application/pdf":
		return KindPDF
	case strings.HasPrefix(mimeType, "text/"):
		return KindText
	default:
		return ""
	}
}

func BuildPublicURL(baseURL string, objectKey string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	objectKey = strings.TrimLeft(strings.TrimSpace(objectKey), "/")
	if baseURL == "" {
		return objectKey
	}
	if objectKey == "" {
		return baseURL
	}
	return baseURL + "/" + objectKey
}

func NewObjectKey(sessionID string, assetID string, filename string) string {
	sessionID = objectKeySegment(sessionID, "unknown-session")
	assetID = objectKeySegment(assetID, "unknown-asset")
	return path.Join("aigc", "sessions", sessionID, "assets", assetID, sanitizeFilename(filename))
}

func objectKeySegment(value string, fallback string) string {
	value = strings.Trim(strings.TrimSpace(value), "/")
	if value == "" {
		return fallback
	}
	return value
}

func sanitizeFilename(filename string) string {
	filename = filepath.Base(strings.TrimSpace(filename))
	if filename == "." || filename == "/" || filename == "" {
		return "asset.bin"
	}
	ext := strings.ToLower(filepath.Ext(filename))
	name := strings.TrimSuffix(filename, filepath.Ext(filename))
	name = slug(name)
	if name == "" {
		name = "asset"
	}
	ext = slugExt(ext)
	if ext == "" {
		return name
	}
	return name + "." + ext
}

func slugExt(ext string) string {
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	var b strings.Builder
	for _, r := range ext {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func slug(value string) string {
	var parts []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		parts = append(parts, current.String())
		current.Reset()
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			current.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			current.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			current.WriteRune(r)
		default:
			flush()
			if mapped, ok := chineseSlug[r]; ok {
				parts = append(parts, mapped)
			}
		}
	}
	flush()
	return strings.Join(parts, "-")
}

var chineseSlug = map[rune]string{
	'苏': "su",
	'寂': "ji",
	'参': "can",
	'考': "kao",
	'图': "tu",
}
