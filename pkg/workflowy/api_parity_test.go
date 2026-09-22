package workflowy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mholzen/workflowy/pkg/client"
	"github.com/stretchr/testify/require"
)

const paritySource = "11111111-1111-4111-8111-111111111111"
const parityParent = "22222222-2222-4222-8222-222222222222"
const parityMirror = "33333333-3333-4333-8333-333333333333"

// Every operation in https://workflowy.com/api-reference.md has an explicit case.
func TestPublicAPICoverage(t *testing.T) {
	ctx := context.Background()
	bottom, empty, layout := "bottom", "", "quote-block"
	tests := []struct {
		name, method, path, body, response string
		call                               func(*WorkflowyClient) (any, error)
	}{
		{"create", "POST", "/nodes", `{"parent_id":"` + parityParent + `","name":"**hello**","layoutMode":"quote-block","position":"bottom"}`, `{"item_id":"` + paritySource + `"}`, func(c *WorkflowyClient) (any, error) {
			return c.CreateNode(ctx, &CreateNodeRequest{ParentID: parityParent, Name: "**hello**", LayoutMode: &layout, Position: &bottom})
		}},
		{"update", "POST", "/nodes/" + paritySource, `{"note":""}`, `{"status":"ok"}`, func(c *WorkflowyClient) (any, error) {
			return c.UpdateNode(ctx, paritySource, &UpdateNodeRequest{Note: &empty})
		}},
		{"retrieve", "GET", "/nodes/" + paritySource, "", `{"node":{"id":"` + paritySource + `","parent_id":"` + parityParent + `","name":"hello","completed":true,"completedAt":123}}`, func(c *WorkflowyClient) (any, error) { return c.GetItem(ctx, paritySource) }},
		{"list", "GET", "/nodes?parent_id=" + parityParent, "", `{"nodes":[{"id":"` + paritySource + `","name":"hello","parent_id":"` + parityParent + `","completed":true}]}`, func(c *WorkflowyClient) (any, error) { return c.ListChildren(ctx, parityParent) }},
		{"delete", "DELETE", "/nodes/" + paritySource, "", `{"status":"ok"}`, func(c *WorkflowyClient) (any, error) { return c.DeleteNode(ctx, paritySource) }},
		{"move", "POST", "/nodes/" + paritySource + "/move", `{"parent_id":"` + parityParent + `","position":"bottom"}`, `{"status":"ok"}`, func(c *WorkflowyClient) (any, error) {
			return c.MoveNode(ctx, paritySource, &MoveNodeRequest{ParentID: parityParent, Position: &bottom})
		}},
		{"mirror", "POST", "/nodes/" + parityMirror + "/mirror", `{"parent_id":"` + parityParent + `","position":"bottom"}`, `{"item_id":"44444444-4444-4444-8444-444444444444","origin_id":"` + paritySource + `"}`, func(c *WorkflowyClient) (any, error) {
			return c.MirrorNode(ctx, parityMirror, &MirrorNodeRequest{ParentID: parityParent, Position: &bottom})
		}},
		{"delete-mirror", "DELETE", "/nodes/" + parityMirror + "/mirror", "", `{"status":"ok"}`, func(c *WorkflowyClient) (any, error) { return c.DeleteMirror(ctx, parityMirror) }},
		{"complete", "POST", "/nodes/" + paritySource + "/complete", "", `{"status":"ok"}`, func(c *WorkflowyClient) (any, error) { return c.CompleteNode(ctx, paritySource) }},
		{"uncomplete", "POST", "/nodes/" + paritySource + "/uncomplete", "", `{"status":"ok"}`, func(c *WorkflowyClient) (any, error) { return c.UncompleteNode(ctx, paritySource) }},
		{"export", "GET", "/nodes-export", "", `{"nodes":[{"id":"` + paritySource + `","parent_id":"` + parityParent + `","name":"hello","completed":true}]}`, func(c *WorkflowyClient) (any, error) { return c.ExportNodes(ctx) }},
		{"targets", "GET", "/targets", "", `{"targets":[{"key":"inbox","type":"system","name":null}]}`, func(c *WorkflowyClient) (any, error) { return c.ListTargets(ctx) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, tt.method, r.Method)
				require.Equal(t, tt.path, r.URL.RequestURI())
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				if tt.body != "" {
					var body json.RawMessage
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.JSONEq(t, tt.body, string(body))
					require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				}
				fmt.Fprint(w, tt.response)
			}))
			defer s.Close()
			c := &WorkflowyClient{Client: client.New(s.URL, WithAPIKey("test-token"))}
			out, err := tt.call(c)
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			encoded, err := json.Marshal(out)
			require.NoError(t, err)
			var actual, expected map[string]any
			require.NoError(t, json.Unmarshal(encoded, &actual))
			require.NoError(t, json.Unmarshal([]byte(tt.response), &expected))
			if tt.name == "retrieve" {
				expected = expected["node"].(map[string]any)
			}
			// Response decoders may add zero-valued fields, but must retain API fields.
			for key, value := range expected {
				if key == "nodes" {
					rows := actual[key].([]any)
					want := value.([]any)
					require.Len(t, rows, len(want))
					for i, row := range want {
						for k, v := range row.(map[string]any) {
							require.Equal(t, v, rows[i].(map[string]any)[k])
						}
					}
				} else {
					require.Equal(t, value, actual[key], key)
				}
			}
		})
	}
}

func TestMirrorValidationAndServerErrors(t *testing.T) {
	ctx := context.Background()
	for _, status := range []int{400, 403, 404, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":"not a mirror or invalid destination"}`)
			}))
			defer s.Close()
			c := &WorkflowyClient{Client: client.New(s.URL)}
			_, err := c.MirrorNode(ctx, paritySource, &MirrorNodeRequest{ParentID: parityParent})
			var api *client.APIError
			require.True(t, errors.As(err, &api))
			require.Equal(t, status, api.Status)
			require.Equal(t, "60", api.RetryAfter)
			_, err = c.DeleteMirror(ctx, paritySource)
			require.True(t, errors.As(err, &api))
			require.Equal(t, status, api.Status)
			require.Equal(t, 2, calls)
			for _, parent := range []string{"", "None", "inbox", "today", paritySource} {
				_, err = c.MirrorNode(ctx, paritySource, &MirrorNodeRequest{ParentID: parent})
				require.Error(t, err)
			}
			bad := "middle"
			_, err = c.MirrorNode(ctx, paritySource, &MirrorNodeRequest{ParentID: parityParent, Position: &bad})
			require.Error(t, err)
			_, err = c.MirrorNode(ctx, paritySource, nil)
			require.Error(t, err)
			_, err = c.DeleteMirror(ctx, "../nodes")
			require.Error(t, err)
			require.Equal(t, 2, calls)
		})
	}
}

func TestPreserveNodeFieldsAcrossSources(t *testing.T) {
	parent := "parent"
	completed := int64(123)
	item := ExportNodeToItem(ExportNode{ID: "child", ParentID: &parent, Completed: true, CompletedAt: &completed})
	require.Equal(t, &parent, item.ParentID)
	require.True(t, item.Completed)
	backup := BackupNodeToItem(BackupNode{ID: parent, Children: []BackupNode{{ID: "child", Completed: &completed}}})
	require.Equal(t, &parent, backup.Children[0].ParentID)
	require.True(t, backup.Children[0].Completed)
}
