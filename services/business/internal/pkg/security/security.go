package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemory      uint32 = 16 * 1024
	argonIterations  uint32 = 1
	argonParallelism uint8  = 1
	argonKeyLen      uint32 = 32
)

func RandomID(prefix string) string {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		sum := sha256.Sum256([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
		return prefix + hex.EncodeToString(sum[:8])
	}
	return prefix + hex.EncodeToString(data)
}

func NewOpaqueToken(prefix string) (string, string) {
	raw := prefix + "_" + RandomID("")
	return raw, HashOpaque(raw)
}

func HashOpaque(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func HashIdentifier(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", fmt.Errorf("password must be at least 8 characters")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

func VerifyPassword(stored, password string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	params := map[string]string{}
	for _, item := range strings.Split(parts[3], ",") {
		kv := strings.SplitN(item, "=", 2)
		if len(kv) == 2 {
			params[kv[0]] = kv[1]
		}
	}
	memory, err := parseUint32(params["m"])
	if err != nil {
		return false
	}
	iterations, err := parseUint32(params["t"])
	if err != nil {
		return false
	}
	parallelism64, err := strconv.ParseUint(params["p"], 10, 8)
	if err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, uint8(parallelism64), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func parseUint32(value string) (uint32, error) {
	parsed, err := strconv.ParseUint(value, 10, 32)
	return uint32(parsed), err
}

func MaskEmail(email string) string {
	email = strings.TrimSpace(email)
	parts := strings.Split(email, "@")
	if len(parts) != 2 || parts[0] == "" {
		return ""
	}
	local := parts[0]
	if len(local) <= 2 {
		return local[:1] + "***@" + parts[1]
	}
	return local[:1] + "***" + local[len(local)-1:] + "@" + parts[1]
}

func SignPreviewToken(secret, kind, subject string, ttl time.Duration) string {
	expires := time.Now().UTC().Add(ttl).Unix()
	body := fmt.Sprintf("%s:%s:%d", kind, subject, expires)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return body + ":" + hex.EncodeToString(mac.Sum(nil))
}

func VerifyPreviewToken(secret, token, kind, subject string) bool {
	parts := strings.Split(token, ":")
	if len(parts) < 4 {
		return false
	}
	tokenKind := parts[0]
	tokenSubject := strings.Join(parts[1:len(parts)-2], ":")
	tokenExpires := parts[len(parts)-2]
	tokenMAC := parts[len(parts)-1]
	if tokenKind != kind || tokenSubject != subject {
		return false
	}
	expires, err := strconv.ParseInt(tokenExpires, 10, 64)
	if err != nil || time.Now().UTC().Unix() > expires {
		return false
	}
	body := strings.Join(parts[:len(parts)-1], ":")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	expected := hex.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(tokenMAC)) == 1
}
