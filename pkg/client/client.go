package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	baseURL      string
	http         *http.Client
	brokerSocket string                // non-empty when calls travel through the local broker
	auth         func(r *http.Request) // injects auth headers
}

// SetAuth allows setting the auth function after client creation
func (c *Client) SetAuth(authFunc func(r *http.Request)) {
	c.auth = authFunc
}

type Option func(*Client)

// --- local Workflowy broker ------------------------------------------------
//
// When the per-session workflowy-broker is installed and the base URL is a
// Workflowy host, calls travel over the broker's private Unix socket instead
// of the public API, so every local process shares one dispatch order (2 s
// between departures, 65 s between full exports, the 429 queue suspension and
// the never-replay-a-write rule are then enforced centrally).
//
//	WF_BROKER=off       never route, call directly
//	WF_BROKER=required  always route; a missing broker fails the call
//	anything else       route only when the private socket exists

const (
	brokerDefaultTimeoutMs = 15000
	brokerQueueSafetyMs    = 300000
)

var brokerHosts = map[string]bool{
	"workflowy.com":      true,
	"www.workflowy.com":  true,
	"beta.workflowy.com": true,
}

func brokerMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WF_BROKER"))) {
	case "off", "0", "false", "no":
		return "off"
	case "required", "1", "true", "on":
		return "required"
	default:
		return "auto"
	}
}

func brokerHome() string {
	if home := os.Getenv("WF_BROKER_HOME"); home != "" {
		return home
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(userHome, ".local", "share", "workflowy-broker")
}

func brokerSocketPath() string {
	if socket := os.Getenv("WF_BROKER_SOCKET"); socket != "" {
		return socket
	}
	home := brokerHome()
	if home == "" {
		return ""
	}
	return filepath.Join(home, "broker.sock")
}

// brokerInstalled reports whether the broker exists on this machine. Auto mode
// routes as soon as it is installed: an installed broker whose socket is gone
// must fail clearly instead of silently falling back to a direct call.
func brokerInstalled(socket string) bool {
	if _, err := os.Stat(socket); err == nil {
		return true
	}
	home := brokerHome()
	if home == "" {
		return false
	}
	info, err := os.Stat(home)
	return err == nil && info.IsDir()
}

// brokerRoute returns the socket to dial for this base URL, or "" to call
// directly. A non-Workflowy base (local test server, proxy) is never routed.
func brokerRoute(base string) string {
	mode := brokerMode()
	if mode == "off" {
		return ""
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || !brokerHosts[strings.ToLower(parsed.Hostname())] {
		return ""
	}
	socket := brokerSocketPath()
	if socket == "" {
		return ""
	}
	if mode == "auto" && !brokerInstalled(socket) {
		return ""
	}
	return socket
}

func newHTTPClient(base string) (*http.Client, string) {
	socket := brokerRoute(base)
	if socket == "" {
		return &http.Client{Timeout: 30 * time.Second}, "" // always set timeouts
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			conn, err := dialer.DialContext(ctx, "unix", socket)
			if err != nil {
				return nil, fmt.Errorf(
					"workflowy broker unavailable at %s (%s); the request was not sent and no direct call was made (WF_BROKER=%s)",
					socket, err, brokerMode())
			}
			return conn, nil
		},
	}
	// The broker counts the network timeout from dispatch; this client timeout
	// only bounds the wait for a broker that accepted the request and never
	// answered it (queue safety), it is not the request timeout.
	timeout := time.Duration(brokerDefaultTimeoutMs+brokerQueueSafetyMs) * time.Millisecond
	return &http.Client{Timeout: timeout, Transport: transport}, socket
}

func New(base string, opts ...Option) *Client {
	httpClient, socket := newHTTPClient(base)
	c := &Client{
		baseURL:      strings.TrimRight(base, "/"),
		http:         httpClient,
		brokerSocket: socket,
		auth:         func(*http.Request) {},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Do(ctx context.Context, method, path string, in any, out any) error {
	u := c.baseURL + path

	var body io.ReadWriter
	// For GET requests, encode input as query parameters; otherwise use JSON body
	if method == "GET" && in != nil {
		// Query parameters will be handled by caller building the path
		// So we just set in to nil to avoid JSON encoding
		in = nil
	}
	if in != nil {
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(in); err != nil {
			return fmt.Errorf("encode: %w", err)
		}
		body = buf
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.brokerSocket != "" {
		// The broker speaks plain HTTP on the private socket; keeping the host
		// makes it the Host header it validates against its Workflowy allowlist,
		// and the target stays the same https Workflowy URL upstream.
		routed := *req.URL
		routed.Scheme = "http"
		req.URL = &routed
		req.Header.Set("X-WF-Client", "workflowy-go")
		req.Header.Set("X-WF-Op", strings.ToLower(method))
		req.Header.Set("X-WF-Timeout-Ms", strconv.Itoa(brokerDefaultTimeoutMs))
	}

	c.auth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		// include Retry-After for backoff decisions
		return &APIError{Status: resp.StatusCode, Body: string(b), RetryAfter: resp.Header.Get("Retry-After")}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

type APIError struct {
	Status     int
	Body       string
	RetryAfter string
}

func (e *APIError) Error() string { return fmt.Sprintf("api %d: %s", e.Status, e.Body) }
