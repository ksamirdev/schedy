package executor

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ksamirdev/schedy/internal/scheduler"
)

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
