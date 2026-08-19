package admin

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appRuntime "max-proxy-mock/internal/runtime"
	"max-proxy-mock/internal/storage"
)

func TestPACContainsOnlyProjectDomainsAndConfiguredProxy(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.CreateProject(t.Context(), "Web", "api.example.com"); err != nil {
		t.Fatal(err)
	}
	api := New(store, appRuntime.New(), "missing.crt", "http://127.0.0.1:8900/proxy.pac", "127.0.0.1:9999")
	mux := http.NewServeMux()
	api.Register(mux)
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8900/proxy.pac", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != 200 {
		t.Fatalf("unexpected status %d: %s", rec.Code, body)
	}
	if !strings.Contains(body, `api.example.com`) || !strings.Contains(body, `PROXY 127.0.0.1:9999`) {
		t.Fatalf("unexpected PAC: %s", body)
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "proxy-autoconfig") {
		t.Fatalf("unexpected content type: %s", rec.Header().Get("Content-Type"))
	}
}

func TestSystemProxyMutationRejectsCrossOriginRequests(t *testing.T) {
	store, err := storage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api := New(store, appRuntime.New(), "missing.crt", "http://127.0.0.1:8900/proxy.pac", "127.0.0.1:8899")
	mux := http.NewServeMux()
	api.Register(mux)
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8900/api/system-proxy", strings.NewReader(`{"action":"enable"}`))
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}
