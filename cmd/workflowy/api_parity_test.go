package main

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/mholzen/workflowy/pkg/workflowy"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

const testSourceID = "11111111-1111-4111-8111-111111111111"
const testParentID = "22222222-2222-4222-8222-222222222222"
const testMirrorID = "33333333-3333-4333-8333-333333333333"

type parityClient struct {
	MockClient
	reads    []string
	mirrors  int
	deletes  int
	update   *workflowy.UpdateNodeRequest
	request  *workflowy.MirrorNodeRequest
	exported []workflowy.ExportNode
}

func (c *parityClient) GetItem(ctx context.Context, id string) (*workflowy.Item, error) {
	c.reads = append(c.reads, id)
	switch id {
	case testSourceID, "111111111111":
		return &workflowy.Item{ID: testSourceID}, nil
	case testParentID, "222222222222":
		return &workflowy.Item{ID: testParentID}, nil
	case testMirrorID, "333333333333":
		return &workflowy.Item{ID: testMirrorID}, nil
	default:
		return nil, fmt.Errorf("missing node")
	}
}
func (c *parityClient) ListTargets(context.Context) (*workflowy.ListTargetsResponse, error) {
	return &workflowy.ListTargetsResponse{}, nil
}
func (c *parityClient) ExportNodesWithCache(context.Context, bool) (*workflowy.ExportNodesResponse, error) {
	return &workflowy.ExportNodesResponse{Nodes: c.exported}, nil
}
func (c *parityClient) MirrorNode(ctx context.Context, id string, req *workflowy.MirrorNodeRequest) (*workflowy.MirrorNodeResponse, error) {
	c.mirrors++
	c.request = req
	return &workflowy.MirrorNodeResponse{ItemID: testMirrorID, OriginID: testSourceID}, nil
}
func (c *parityClient) DeleteMirror(context.Context, string) (*workflowy.UpdateNodeResponse, error) {
	c.deletes++
	return &workflowy.UpdateNodeResponse{Status: "ok"}, nil
}
func (c *parityClient) UpdateNode(ctx context.Context, id string, req *workflowy.UpdateNodeRequest) (*workflowy.UpdateNodeResponse, error) {
	c.update = req
	return &workflowy.UpdateNodeResponse{Status: "ok"}, nil
}

func runParityCommand(c *parityClient, args ...string) (string, error) {
	var out bytes.Buffer
	root := &cli.Command{Name: "workflowy", Writer: &out, Flags: []cli.Flag{&cli.StringFlag{Name: "format", Value: "json"}, getReadRootIdFlag(), getWriteRootIdFlag()}, Commands: []*cli.Command{
		getMirrorCommandWithClient(withMockClient(c)), getDeleteMirrorCommandWithClient(withMockClient(c)), getUpdateCommandWithClient(withMockClient(c)),
	}}
	err := root.Run(context.Background(), append([]string{"workflowy"}, args...))
	return out.String(), err
}

func TestMirrorCLIResolvesFreshIDsAndReturnsOrigin(t *testing.T) {
	c := &parityClient{}
	out, err := runParityCommand(c, "mirror", "https://workflowy.com/#/111111111111?q=test", "222222222222", "--position=bottom")
	require.NoError(t, err)
	require.Equal(t, []string{"111111111111", "222222222222"}, c.reads)
	require.Equal(t, 1, c.mirrors)
	require.Equal(t, testParentID, c.request.ParentID)
	require.Equal(t, "bottom", *c.request.Position)
	require.JSONEq(t, `{"item_id":"`+testMirrorID+`","origin_id":"`+testSourceID+`"}`, out)
}

func TestMirrorCLIRejectsInvalidInputsAndUnverifiableScope(t *testing.T) {
	for _, args := range [][]string{
		{"mirror", testSourceID, "inbox"},
		{"mirror", testSourceID, "today"},
		{"mirror", testSourceID, "None"},
		{"mirror", testSourceID, testParentID, "--position=middle"},
		{"mirror"},
		{"--read-root-id=" + testParentID, "mirror", testSourceID, testParentID},
		{"--write-root-id=" + testParentID, "mirror", testSourceID, testParentID},
	} {
		t.Run(fmt.Sprint(args), func(t *testing.T) {
			c := &parityClient{}
			_, err := runParityCommand(c, args...)
			require.Error(t, err)
			require.Zero(t, c.mirrors)
		})
	}
}

func TestDeleteMirrorCLIAndScope(t *testing.T) {
	for _, inside := range []bool{true, false} {
		t.Run(fmt.Sprint(inside), func(t *testing.T) {
			parent := testParentID
			if !inside {
				parent = testSourceID
			}
			c := &parityClient{exported: []workflowy.ExportNode{{ID: testParentID, Name: "scope"}, {ID: testMirrorID, ParentID: &parent}}}
			out, err := runParityCommand(c, "--write-root-id="+testParentID, "--read-root-id="+testParentID, "delete-mirror", "333333333333")
			if inside {
				require.NoError(t, err)
				require.Equal(t, 1, c.deletes)
				require.JSONEq(t, `{"status":"ok"}`, out)
			} else {
				require.Error(t, err)
				require.Zero(t, c.deletes)
			}
		})
	}
}

func TestUpdateCLIExplicitEmptyAndOmitted(t *testing.T) {
	for _, field := range []string{"name", "note"} {
		t.Run(field, func(t *testing.T) {
			c := &parityClient{}
			_, err := runParityCommand(c, "update", testSourceID, "--"+field, "")
			require.NoError(t, err)
			require.NotNil(t, c.update)
			if field == "note" {
				require.NotNil(t, c.update.Note)
				require.Empty(t, *c.update.Note)
				require.Nil(t, c.update.Name)
			} else {
				require.NotNil(t, c.update.Name)
				require.Empty(t, *c.update.Name)
				require.Nil(t, c.update.Note)
			}
		})
	}
	c := &parityClient{}
	_, err := runParityCommand(c, "update", testSourceID)
	require.Error(t, err)
	require.Nil(t, c.update)
	for _, layout := range []string{"code-block", "quote-block"} {
		c := &parityClient{}
		_, err := runParityCommand(c, "update", testSourceID, "--layout-mode="+layout)
		require.NoError(t, err)
		require.Equal(t, layout, *c.update.LayoutMode)
	}
}
