package workflowy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExportNodesWithCacheUsesBrokerSnapshotAndHonorsBypass(t *testing.T) {
	home, err := os.MkdirTemp("", "wfc")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	socket := filepath.Join(home, "broker.sock")
	listener, err := net.Listen("unix", socket)
	require.NoError(t, err)

	var requests atomic.Int32
	cacheModes := make(chan string, 4)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cacheModes <- r.Header.Get("X-WF-Cache-Mode")
		sequence := requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ExportNodesResponse{
			Nodes: []ExportNode{{ID: fmt.Sprintf("node-%d", sequence), Name: fmt.Sprintf("snapshot-%d", sequence)}},
		})
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })

	t.Setenv("HOME", home)
	t.Setenv("WF_BROKER", "required")
	t.Setenv("WF_BROKER_HOME", home)
	t.Setenv("WF_BROKER_SOCKET", socket)
	t.Setenv("DONT_USE_CACHE", "false")
	wf := NewWorkflowyClient(WithAPIKey("test-token"))

	first, err := wf.ExportNodesWithCache(context.Background(), false)
	require.NoError(t, err)
	second, err := wf.ExportNodesWithCache(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, "snapshot-1", first.Nodes[0].Name)
	require.Equal(t, "snapshot-2", second.Nodes[0].Name, "the per-process disk cache must not mask broker refreshes")

	forced, err := wf.ExportNodesWithCache(context.Background(), true)
	require.NoError(t, err)
	require.Equal(t, "snapshot-3", forced.Nodes[0].Name)
	t.Setenv("DONT_USE_CACHE", "true")
	envForced, err := wf.ExportNodesWithCache(context.Background(), false)
	require.NoError(t, err)
	require.Equal(t, "snapshot-4", envForced.Nodes[0].Name)
	require.Equal(t, int32(4), requests.Load())
	require.Equal(t, []string{"", "", "bypass", "bypass"}, []string{<-cacheModes, <-cacheModes, <-cacheModes, <-cacheModes})
}
