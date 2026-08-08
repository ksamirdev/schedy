package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func corsGet(t *testing.T, origins, origin, method string, preflight bool) *httptest.ResponseRecorder {
	t.Helper()
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	req := httptest.NewRequest(method, "/tasks", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if preflight {
		req.Header.Set("Access-Control-Request-Method", "POST")
	}
	rec := httptest.NewRecorder()
	CORS(origins, inner).ServeHTTP(rec, req)
	return rec
}

func TestCORSDisabledByDefault(t *testing.T) {
	rec := corsGet(t, "", "https://app.example.com", http.MethodGet, false)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS headers when unset, got %q", got)
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected passthrough, got %d", rec.Code)
	}
}

func TestCORSAllowsListedOrigin(t *testing.T) {
	rec := corsGet(t, "https://a.com, https://b.com", "https://b.com", http.MethodGet, false)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://b.com" {
		t.Fatalf("expected origin echoed, got %q", got)
	}
	if rec.Code != http.StatusTeapot {
		t.Fatalf("expected request to reach handler, got %d", rec.Code)
	}
}

func TestCORSRejectsUnlistedOrigin(t *testing.T) {
	rec := corsGet(t, "https://a.com", "https://evil.com", http.MethodGet, false)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no allow-origin for unlisted origin, got %q", got)
	}
}

func TestCORSWildcard(t *testing.T) {
	rec := corsGet(t, "*", "https://anything.dev", http.MethodGet, false)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://anything.dev" {
		t.Fatalf("expected origin echoed under wildcard, got %q", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	rec := corsGet(t, "https://a.com", "https://a.com", http.MethodOptions, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 preflight, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got == "" {
		t.Fatal("expected Allow-Headers on preflight")
	}
	if rec.Header().Get("Vary") != "Origin" {
		t.Fatal("expected Vary: Origin")
	}
}
