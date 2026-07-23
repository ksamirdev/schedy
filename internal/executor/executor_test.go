package executor

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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

// Verifies the HMAC signature headers: present and correct when a secret is
// set (signed over "<timestamp>.<body>"), absent when it is not.
func TestExecuteSignsRequest(t *testing.T) {
	const secret = "topsecret"

	capture := func() (*httptest.Server, *http.Header, *[]byte) {
		hdr := &http.Header{}
		var gotBody []byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*hdr = r.Header.Clone()
			gotBody, _ = io.ReadAll(r.Body)
		}))
		return srv, hdr, &gotBody
	}

	t.Run("signs over timestamp.body when a secret is set", func(t *testing.T) {
		srv, hdr, gotBody := capture()
		defer srv.Close()

		e := NewExecutor()
		e.signingSecret = secret
		res := e.Execute(scheduler.Task{URL: srv.URL, Payload: map[string]string{"hello": "world"}})
		if res.Err != nil {
			t.Fatalf("unexpected err: %v", res.Err)
		}

		ts := hdr.Get("X-Schedy-Timestamp")
		if ts == "" {
			t.Fatal("missing X-Schedy-Timestamp")
		}
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(ts + "." + string(*gotBody)))
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if got := hdr.Get("X-Schedy-Signature"); got != want {
			t.Errorf("signature=%q want %q", got, want)
		}
	})

	t.Run("no signature headers without a secret", func(t *testing.T) {
		srv, hdr, _ := capture()
		defer srv.Close()

		res := NewExecutor().Execute(scheduler.Task{URL: srv.URL, Payload: "hi"})
		if res.Err != nil {
			t.Fatalf("unexpected err: %v", res.Err)
		}
		if sig := hdr.Get("X-Schedy-Signature"); sig != "" {
			t.Errorf("unexpected signature %q", sig)
		}
		if ts := hdr.Get("X-Schedy-Timestamp"); ts != "" {
			t.Errorf("unexpected timestamp %q", ts)
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
