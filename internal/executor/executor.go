package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/ksamirdev/schedy/internal/scheduler"
)

// maxBodyCapture bounds how much of a failed response body we read into the
// attempt log. Enough to see the error message, small enough to keep records lean.
const maxBodyCapture = 2048

// Result is the outcome of a single delivery attempt.
type Result struct {
	StatusCode int           // HTTP status, 0 on transport error
	Err        error         // nil on 2xx, otherwise transport error or non-2xx
	Duration   time.Duration // round-trip time
	// ResponseBody holds up to maxBodyCapture bytes of the response body,
	// captured only on non-2xx responses (empty on success/transport error).
	ResponseBody          string
	ResponseBodyTruncated bool // true if the body exceeded maxBodyCapture
}

type Executor struct {
	client *http.Client
}

// NewExecutor builds the delivery client. Dials to private, loopback,
// link-local (incl. 169.254.169.254 cloud metadata) and unspecified addresses
// are rejected so schedy cannot be used as an SSRF proxy into its host's
// network. Set SCHEDY_ALLOW_PRIVATE_TARGETS to opt out for trusted self-hosted
// setups.
func NewExecutor() *Executor {
	return newExecutor(os.Getenv("SCHEDY_ALLOW_PRIVATE_TARGETS") == "")
}

func newExecutor(guardPrivate bool) *Executor {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if guardPrivate {
		dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		// Control runs after DNS resolution on the concrete IP being dialed, so
		// it also defeats DNS-rebind and redirect-to-metadata that a pre-resolve
		// URL check would miss.
		dialer.Control = blockPrivateDial
		transport.DialContext = dialer.DialContext
	}
	return &Executor{
		client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: transport,
		},
	}
}

// blockPrivateDial rejects any dial to a non-public address.
func blockPrivateDial(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("cannot parse dial address %q", address)
	}
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("blocked dial to non-public address %s", ip)
	}
	return nil
}

// Execute delivers one HTTP request for the task (task.Method, default POST) and reports the attempt outcome
// (status code, error, duration). A 2xx yields a nil Err.
func (e *Executor) Execute(task scheduler.Task) Result {
	method := task.Method
	if method == "" {
		method = http.MethodPost
	}

	var body io.Reader
	// GET/HEAD carry no request body.
	if method != http.MethodGet && method != http.MethodHead {
		var bodyBytes []byte
		switch v := task.Payload.(type) {
		case string:
			bodyBytes = []byte(v)
		case []byte:
			bodyBytes = v
		default:
			// fallback to JSON
			bodyBytes, _ = json.Marshal(task.Payload)
		}
		body = bytes.NewBuffer(bodyBytes)
	}

	req, err := http.NewRequest(method, task.URL, body)
	if err != nil {
		return Result{Err: err}
	}

	// Set custom headers
	for k, v := range task.Headers {
		req.Header.Set(k, v)
	}
	// If no Content-Type header is set, default to application/json (only when
	// there is a body to describe).
	if body != nil && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	res, err := e.client.Do(req)
	dur := time.Since(start)
	if err != nil {
		// transport failure (DNS, timeout, connection refused): res is nil.
		return Result{Err: err, Duration: dur}
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		// Capture the first maxBodyCapture bytes to explain the failure. Read one
		// extra byte so we can flag truncation without a second read.
		buf, _ := io.ReadAll(io.LimitReader(res.Body, maxBodyCapture+1))
		truncated := len(buf) > maxBodyCapture
		if truncated {
			buf = buf[:maxBodyCapture]
		}
		return Result{
			StatusCode:            res.StatusCode,
			Err:                   fmt.Errorf("unexpected status code: %d", res.StatusCode),
			Duration:              dur,
			ResponseBody:          string(buf),
			ResponseBodyTruncated: truncated,
		}
	}

	return Result{StatusCode: res.StatusCode, Duration: dur}
}
