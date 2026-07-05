package asset

import "testing"

func TestBuildPublicURL(t *testing.T) {
	got := BuildPublicURL("https://tos.doraigc.com/", "/aigc/sessions/s1/assets/a1/image.png")
	want := "https://tos.doraigc.com/aigc/sessions/s1/assets/a1/image.png"
	if got != want {
		t.Fatalf("BuildPublicURL() = %q, want %q", got, want)
	}
}

func TestNewObjectKeySanitizesFilename(t *testing.T) {
	got := NewObjectKey("s1", "asset-1", "../苏寂 参考图.png")
	want := "aigc/sessions/s1/assets/asset-1/su-ji-can-kao-tu.png"
	if got != want {
		t.Fatalf("NewObjectKey() = %q, want %q", got, want)
	}
}

func TestKindFromMIME(t *testing.T) {
	cases := map[string]string{
		"image/png":       KindImage,
		"audio/mpeg":      KindAudio,
		"video/mp4":       KindVideo,
		"application/pdf": KindPDF,
		"text/plain":      KindText,
	}
	for mimeType, want := range cases {
		if got := KindFromMIME(mimeType); got != want {
			t.Fatalf("KindFromMIME(%q) = %q, want %q", mimeType, got, want)
		}
	}
}
