package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/FigoGoo/Dora-Agent/business/internal/mediapreview"
	"github.com/gin-gonic/gin"
)

type mediaPreviewHTTPRepositoryStub struct{}

func (mediaPreviewHTTPRepositoryStub) Prepare(context.Context, mediapreview.PrepareCommand, mediapreview.PreparationAllocation) (mediapreview.PrepareResult, error) {
	return mediapreview.PrepareResult{}, mediapreview.ErrNotFound
}
func (mediaPreviewHTTPRepositoryStub) QueryPreparation(context.Context, mediapreview.PreparationQuery) (mediapreview.PreparationQueryResult, error) {
	return mediapreview.PreparationQueryResult{Status: mediapreview.QueryStatusNotFound}, nil
}
func (mediaPreviewHTTPRepositoryStub) Finalize(context.Context, mediapreview.FinalizeCommand, mediapreview.FinalizationAllocation) (mediapreview.FinalizeResult, error) {
	return mediapreview.FinalizeResult{}, mediapreview.ErrNotFound
}
func (mediaPreviewHTTPRepositoryStub) QueryFinalization(context.Context, mediapreview.FinalizationQuery) (mediapreview.FinalizationQueryResult, error) {
	return mediapreview.FinalizationQueryResult{Status: mediapreview.QueryStatusNotFound}, nil
}
func (mediaPreviewHTTPRepositoryStub) OpenReadyContent(context.Context, mediapreview.ContentQuery) (mediapreview.ReadyContent, *os.File, error) {
	return mediapreview.ReadyContent{}, nil, mediapreview.ErrNotFound
}

type mediaPreviewHTTPClock struct{}

func (mediaPreviewHTTPClock) Now() time.Time { return time.Now().UTC() }

func TestMediaPreviewInternalReadinessIsLoopbackOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := mediapreview.OpenLocalObjectStore(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	service, err := mediapreview.NewService(mediaPreviewHTTPRepositoryStub{}, mediaPreviewHTTPClock{}, agentProxyIDs{})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewMediaPreviewHandler(service, store, agentProxyIDs{})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler.RegisterInternal(router)

	request := httptest.NewRequest(http.MethodGet, "/internal/v1/media-preview-assets/readiness", nil)
	request.RemoteAddr = "127.0.0.1:32100"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("loopback status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil ||
		response["schema_version"] != mediapreview.ReadinessSchemaVersion ||
		response["profile"] != mediapreview.RuntimeProfile || response["object_root_ready"] != true {
		t.Fatalf("readiness response=%v error=%v", response, err)
	}

	request = httptest.NewRequest(http.MethodGet, "/internal/v1/media-preview-assets/readiness", nil)
	request.RemoteAddr = "192.0.2.10:32100"
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound || recorder.Body.Len() != 0 {
		t.Fatalf("external readiness status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestParseSingleMediaRange(t *testing.T) {
	tests := []struct {
		values        []string
		size          int64
		start, end    int64
		partial, okay bool
	}{
		{nil, 10, 0, 9, false, true},
		{[]string{"bytes=0-"}, 10, 0, 9, true, true},
		{[]string{"bytes=2-"}, 10, 2, 9, true, true},
		{[]string{"bytes=2-5"}, 10, 2, 5, true, true},
		{[]string{"bytes=8-99"}, 10, 8, 9, true, true},
		{[]string{"bytes=2-", "bytes=3-4"}, 10, 0, 0, false, false},
		{[]string{"bytes=2-3,5-6"}, 10, 0, 0, false, false},
		{[]string{"bytes=-3"}, 10, 0, 0, false, false},
		{[]string{"bytes=10-11"}, 10, 0, 0, false, false},
	}
	for _, test := range tests {
		start, end, partial, okay := parseSingleMediaRange(test.values, test.size)
		if start != test.start || end != test.end || partial != test.partial || okay != test.okay {
			t.Fatalf("range=%v size=%d got=(%d,%d,%v,%v)", test.values, test.size, start, end, partial, okay)
		}
	}
}
