package workflowy

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// MirrorNodeRequest shares the API's placement contract with MoveNodeRequest.
type MirrorNodeRequest = MoveNodeRequest

type MirrorNodeResponse struct {
	ItemID   string `json:"item_id"`
	OriginID string `json:"origin_id"`
}

var fullNodeID = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)

// ResolveExistingNodeUUID resolves an ID using a fresh GET, not the export cache.
// Mirror destinations must already exist; this never creates calendar nodes.
func ResolveExistingNodeUUID(ctx context.Context, c Client, id string) (string, error) {
	id = strings.TrimSpace(id)
	if strings.HasPrefix(id, "https://workflowy.com/#/") {
		u, err := url.Parse(id)
		if err != nil {
			return "", err
		}
		id = strings.SplitN(strings.TrimPrefix(u.Fragment, "/"), "?", 2)[0]
	}
	if !fullNodeID.MatchString(id) && !IsShortID(id) {
		return "", fmt.Errorf("expected a full UUID, short node ID or Workflowy node URL; targets and root are not supported")
	}
	item, err := c.GetItem(ctx, id)
	if err != nil {
		return "", err
	}
	if item == nil || !fullNodeID.MatchString(item.ID) {
		return "", fmt.Errorf("API did not return a full node UUID")
	}
	return item.ID, nil
}

func (wc *WorkflowyClient) MirrorNode(ctx context.Context, itemID string, req *MirrorNodeRequest) (*MirrorNodeResponse, error) {
	if !fullNodeID.MatchString(itemID) || req == nil || !fullNodeID.MatchString(req.ParentID) {
		return nil, fmt.Errorf("mirror source and parent must be full existing node UUIDs")
	}
	if req.ParentID == itemID {
		return nil, fmt.Errorf("cannot mirror a node into itself")
	}
	if req.Position != nil {
		if err := ValidatePosition(*req.Position); err != nil {
			return nil, err
		}
	}
	var resp MirrorNodeResponse
	if err := wc.Do(ctx, "POST", "/nodes/"+itemID+"/mirror", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteMirror only removes a mirror instance. The server rejects ordinary nodes.
func (wc *WorkflowyClient) DeleteMirror(ctx context.Context, itemID string) (*UpdateNodeResponse, error) {
	if !fullNodeID.MatchString(itemID) {
		return nil, fmt.Errorf("mirror ID must be a full node UUID")
	}
	var resp UpdateNodeResponse
	if err := wc.Do(ctx, "DELETE", "/nodes/"+itemID+"/mirror", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
