package executor

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ksamirdev/schedy/internal/scheduler"
)

// httptest servers bind to loopback, which the SSRF guard blocks by default.
// Opt out for the package so NewExecutor() can reach them; the guard itself is
// exercised directly via newExecutor(true) in TestExecuteBlocksPrivateTargets.
func TestMain(m *testing.M) {
	os.Setenv("SCHEDY_ALLOW_PRIVATE_TARGETS", "1")
	os.Exit(m.Run())
}

// Verifies the dial-time guard rejects a loopback target when enabled.
func TestExecuteBlocksPrivateTargets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	res := newExecutor(true).Execute(scheduler.Task{URL: srv.URL})
	if res.Err == nil {
		t.Fatal("expected loopback dial to be blocked")
	}
	if !strings.Contains(res.Err.Error(), "blocked dial to non-public address") {
		t.Errorf("unexpected error: %v", res.Err)
	}
}

// Verifies the response body is captured (and truncation flagged) on non-2xx
// responses, and left empty on success.
func TestExecuteCapturesFailedBody(t *testing.T) {
	t.Run("non-2xx captures body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, "boom: bad payload")
		}))
		defer srv.Close()

		res := NewExecutor().Execute(scheduler.Task{URL: srv.URL})
		if res.Err == nil {
			t.Fatal("expected error on 400")
		}
		if res.ResponseBody != "boom: bad payload" || res.ResponseBodyTruncated {
			t.Errorf("body=%q truncated=%v", res.ResponseBody, res.ResponseBodyTruncated)
		}
	})

	t.Run("oversized body truncated", func(t *testing.T) {
		big := strings.Repeat("x", maxBodyCapture+500)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			io.WriteString(w, big)
		}))
		defer srv.Close()

		res := NewExecutor().Execute(scheduler.Task{URL: srv.URL})
		if len(res.ResponseBody) != maxBodyCapture || !res.ResponseBodyTruncated {
			t.Errorf("len=%d truncated=%v", len(res.ResponseBody), res.ResponseBodyTruncated)
		}
	})

	t.Run("success captures nothing", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "ok")
		}))
		defer srv.Close()

		res := NewExecutor().Execute(scheduler.Task{URL: srv.URL})
		if res.Err != nil || res.ResponseBody != "" {
			t.Errorf("err=%v body=%q", res.Err, res.ResponseBody)
		}
	})
}

// Verifies task.Method is honored and GET/HEAD carry no body.
func TestExecuteMethodAndBody(t *testing.T) {
	cases := []struct {
		method   string
		wantBody bool
	}{
		{"", true}, // default POST
		{http.MethodPut, true},
		{http.MethodGet, false},
		{http.MethodHead, false},
	}
	for _, c := range cases {
		var gotMethod string
		var gotBody []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotMethod = r.Method
			gotBody, _ = io.ReadAll(r.Body)
		}))

		res := NewExecutor().Execute(scheduler.Task{URL: srv.URL, Method: c.method, Payload: "hi"})
		if res.Err != nil {
			t.Fatalf("method %q: unexpected err: %v", c.method, res.Err)
		}
		wantMethod := c.method
		if wantMethod == "" {
			wantMethod = http.MethodPost
		}
		if gotMethod != wantMethod {
			t.Errorf("method %q: server saw %q", c.method, gotMethod)
		}
		if hasBody := len(gotBody) > 0; hasBody != c.wantBody {
			t.Errorf("method %q: hasBody=%v want %v", c.method, hasBody, c.wantBody)
		}
		srv.Close()
	}
}
