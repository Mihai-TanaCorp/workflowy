package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrokerRouteOnlyTargetsWorkflowyHosts(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "broker.sock")

	t.Setenv("WF_BROKER_SOCKET", socket)

	t.Setenv("WF_BROKER", "off")
	assert.Empty(t, brokerRoute("https://workflowy.com/api/v1"), "off must never route")

	t.Setenv("WF_BROKER", "required")
	assert.Equal(t, socket, brokerRoute("https://workflowy.com/api/v1"))
	assert.Equal(t, socket, brokerRoute("https://beta.workflowy.com/api/v1"))
	// A local test server or proxy is never sent to the broker, whatever the mode.
	assert.Empty(t, brokerRoute("http://127.0.0.1:9999/api/v1"))
	assert.Empty(t, brokerRoute("https://example.com/api/v1"))

	t.Setenv("WF_BROKER", "auto")
	assert.Empty(t, brokerRoute("https://workflowy.com/api/v1"), "auto must not route without a socket")
	require.NoError(t, os.WriteFile(socket, nil, 0o600))
	assert.Equal(t, socket, brokerRoute("https://workflowy.com/api/v1"), "auto routes once the socket exists")
}

// End-to-end proof that a Workflowy call travels over the private socket and
// still reaches the upstream with the same payload. Opt-in:
//
//	WF_BROKER_SMOKE_BROKER=/path/to/broker.js go test ./pkg/client -run Broker
func TestBrokerRoutesWorkflowyCallsOverTheSocket(t *testing.T) {
	brokerProgram := os.Getenv("WF_BROKER_SMOKE_BROKER")
	if brokerProgram == "" {
		t.Skip("set WF_BROKER_SMOKE_BROKER to run the broker end-to-end smoke test")
	}

	var received []string
	var lastAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received = append(received, r.Method+" "+r.URL.String()+" "+string(body))
		lastAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"node":{"id":"smoke","name":"via broker"}}`))
	}))
	defer upstream.Close()

	// A short path: unix socket paths are limited to ~104 bytes on macOS, and
	// t.TempDir() with a long test name exceeds it.
	home, err := os.MkdirTemp("", "wfb")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	socket := filepath.Join(home, "broker.sock")
	var brokerLog strings.Builder
	broker := exec.Command("node", brokerProgram)
	broker.Stdout = &brokerLog
	broker.Stderr = &brokerLog
	broker.Env = append(os.Environ(),
		"WF_BROKER_HOME="+home,
		"WF_BROKER_SOCKET="+socket,
		"WF_BROKER_LOG="+filepath.Join(home, "logs", "broker.jsonl"),
		"WF_BROKER_MIN_INTERVAL_MS=20",
		"WF_BROKER_TEST_ORIGIN="+upstream.URL,
	)
	require.NoError(t, broker.Start())
	defer func() {
		_ = broker.Process.Kill()
		_, _ = broker.Process.Wait()
	}()

	require.Eventually(t, func() bool {
		info, err := os.Stat(socket)
		return err == nil && info.Mode()&os.ModeSocket != 0
	}, 10*time.Second, 25*time.Millisecond, "broker socket never appeared: "+brokerLog.String())

	t.Setenv("WF_BROKER", "required")
	t.Setenv("WF_BROKER_SOCKET", socket)

	c := New("https://workflowy.com/api/v1", func(client *Client) {
		client.SetAuth(func(r *http.Request) {
			r.Header.Set("Authorization", "Bearer test-key")
		})
	})

	var out map[string]any
	require.NoError(t, c.Do(context.Background(), "GET", "/nodes/smoke", nil, &out))
	assert.Equal(t, "via broker", out["node"].(map[string]any)["name"])

	require.NoError(t, c.Do(context.Background(), "POST", "/nodes/smoke", map[string]any{"name": "written"}, nil))

	require.Len(t, received, 2)
	assert.True(t, strings.HasPrefix(received[0], "GET /api/v1/nodes/smoke"), received[0])
	assert.True(t, strings.HasPrefix(received[1], "POST /api/v1/nodes/smoke"), received[1])
	assert.Contains(t, received[1], `{"name":"written"}`)
	assert.Equal(t, "Bearer test-key", lastAuth, "the broker must forward the Authorization header")
}
