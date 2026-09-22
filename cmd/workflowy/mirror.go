package main

import (
	"context"
	"fmt"

	"github.com/mholzen/workflowy/pkg/workflowy"
	"github.com/urfave/cli/v3"
)

func getMirrorCommand() *cli.Command { return getMirrorCommandWithClient(withClient) }

func getMirrorCommandWithClient(provider ClientProvider) *cli.Command {
	return &cli.Command{
		Name: "mirror", Usage: "Create a live mirror under an existing node",
		UsageText: "workflowy mirror <id> <parent-id> [--position top|bottom]",
		Arguments: []cli.Argument{&cli.StringArg{Name: "id"}, &cli.StringArg{Name: "parent_id"}},
		Flags:     []cli.Flag{getAPIKeyFlag(), &cli.StringFlag{Name: "position", Usage: "top or bottom (default: top)"}},
		Action: provider(func(ctx context.Context, cmd *cli.Command, c workflowy.Client) error {
			if err := validateFormat(cmd.String("format")); err != nil {
				return err
			}
			// The server redirects mirror sources AND destinations to their origins,
			// but GET/export do not expose those origins. Never silently bypass a scope.
			if workflowy.IsRestricted(getReadRootID(cmd)) || workflowy.IsRestricted(getWriteRootID(cmd)) {
				return fmt.Errorf("mirror unavailable with read-root-id or write-root-id: the API does not expose mirror origins needed to verify the effective scope")
			}
			req := &workflowy.MirrorNodeRequest{}
			if err := req.SetPosition(cmd.String("position")); err != nil {
				return err
			}
			id, err := workflowy.ResolveExistingNodeUUID(ctx, c, cmd.StringArg("id"))
			if err != nil {
				return fmt.Errorf("cannot resolve source: %w", err)
			}
			req.ParentID, err = workflowy.ResolveExistingNodeUUID(ctx, c, cmd.StringArg("parent_id"))
			if err != nil {
				return fmt.Errorf("cannot resolve parent: %w", err)
			}
			resp, err := c.MirrorNode(ctx, id, req)
			if err != nil {
				return err
			}
			if cmd.String("format") == "json" {
				printJSONToWriter(cmd.Root().Writer, resp)
			} else {
				fmt.Fprintf(cmd.Root().Writer, "%s mirrored to %s (origin %s)\n", resp.ItemID, req.ParentID, resp.OriginID)
			}
			return nil
		}),
	}
}

func getDeleteMirrorCommand() *cli.Command { return getDeleteMirrorCommandWithClient(withClient) }

func getDeleteMirrorCommandWithClient(provider ClientProvider) *cli.Command {
	return &cli.Command{
		Name: "delete-mirror", Usage: "Delete only a mirror instance, preserving its origin",
		UsageText: "workflowy delete-mirror <id>",
		Arguments: []cli.Argument{&cli.StringArg{Name: "id"}},
		Flags:     []cli.Flag{getAPIKeyFlag()},
		Action: provider(func(ctx context.Context, cmd *cli.Command, c workflowy.Client) error {
			if err := validateFormat(cmd.String("format")); err != nil {
				return err
			}
			id, err := workflowy.ResolveExistingNodeUUID(ctx, c, cmd.StringArg("id"))
			if err != nil {
				return err
			}
			read, err := NewReadGuard(ctx, c, getReadRootID(cmd))
			if err != nil {
				return err
			}
			if err := read.ValidateTarget(id, "delete-mirror"); err != nil {
				return err
			}
			write, err := NewWriteGuard(ctx, c, getWriteRootID(cmd))
			if err != nil {
				return err
			}
			if err := write.ValidateTarget(id, "delete-mirror"); err != nil {
				return err
			}
			resp, err := c.DeleteMirror(ctx, id)
			if err != nil {
				return err
			}
			if cmd.String("format") == "json" {
				printJSONToWriter(cmd.Root().Writer, resp)
			} else {
				fmt.Fprintf(cmd.Root().Writer, "%s mirror deleted\n", id)
			}
			return nil
		}),
	}
}
