package asset

import (
	"testing"
	"time"
)

type stubSigner struct {
	url       string
	gotBucket string
	gotKey    string
	gotCT     string
	gotTTL    time.Duration
}

func (s *stubSigner) PresignPut(bucket, objectKey, contentType string, ttl time.Duration) (string, error) {
	s.gotBucket, s.gotKey, s.gotCT, s.gotTTL = bucket, objectKey, contentType, ttl
	return s.url, nil
}

func fixedNow() time.Time { return time.Unix(1700000000, 0).UTC() }

// 有 signer(真实环境配置了 TOS 凭证)时，uploadURL 必须走预签名、把 bucket/key/content-type/ttl 透传给 signer。
func TestUploadURLUsesSignerWhenPresent(t *testing.T) {
	stub := &stubSigner{url: "https://signed.example/put?sig=x"}
	a := &App{
		tos:    TOSOptions{Bucket: "dora-public", BaseURL: "https://tos.doraigc.com"},
		signer: stub,
		now:    fixedNow,
	}
	key := "local/spaces/sp_x/projects/proj_x/assets/asset_x/original/obj_x.png"
	got := a.uploadURL(key, "image/png", fixedNow().Add(10*time.Minute))
	if got != stub.url {
		t.Fatalf("want signed url %q, got %q", stub.url, got)
	}
	if stub.gotBucket != "dora-public" || stub.gotKey != key || stub.gotCT != "image/png" {
		t.Fatalf("signer args mismatch: %+v", stub)
	}
	if stub.gotTTL != 10*time.Minute {
		t.Fatalf("ttl want 10m, got %s", stub.gotTTL)
	}
}

// 无 signer(本地/测试无 TOS 凭证)时降级到占位 URL，保证不依赖真实 TOS。
func TestUploadURLFallsBackWithoutSigner(t *testing.T) {
	a := &App{
		tos:    TOSOptions{Bucket: "dora-public", BaseURL: "https://tos.doraigc.com"},
		signer: nil,
		now:    fixedNow,
	}
	got := a.uploadURL("k.png", "image/png", fixedNow().Add(10*time.Minute))
	if got != "https://tos.doraigc.com/k.png?upload_token=local-m4" {
		t.Fatalf("fallback url mismatch: %s", got)
	}
}

// 上传凭证有效期必须 clamp 到规范要求的 5-15 分钟。
func TestClampUploadTTL(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want time.Duration
	}{
		{2 * time.Minute, 5 * time.Minute},
		{5 * time.Minute, 5 * time.Minute},
		{10 * time.Minute, 10 * time.Minute},
		{15 * time.Minute, 15 * time.Minute},
		{20 * time.Minute, 15 * time.Minute},
		{-1 * time.Minute, 5 * time.Minute},
	}
	for _, c := range cases {
		if got := clampUploadTTL(c.in); got != c.want {
			t.Fatalf("clampUploadTTL(%s)=%s want %s", c.in, got, c.want)
		}
	}
}
