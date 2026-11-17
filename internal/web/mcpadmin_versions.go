package web

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/i18n"
	"github.com/toddawhittaker/foreman/internal/world"
)

// The game version tools (SPEC 21.7, 10.2 to 10.4, 10.7). They run the
// update and rollback pages' code; both changes are two-step (SPEC 21.8).

// addVersionTools adds the game version tools (SPEC 21.7).
func (s *server) addVersionTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "list_game_versions", Annotations: oneStep,
		Description: "List the Factorio versions that have game images, newest last, marking the stable and experimental releases. " +
			"With world_id, only versions newer than the world's, which update_game_version can move it to, and whether it can be rolled back."}, s.mcpListGameVersions)
	if s.game == nil {
		return
	}
	mcp.AddTool(srv, &mcp.Tool{Name: "update_game_version", Annotations: twoStep,
		Description: "Move a world to a newer Factorio version, in two steps, as the world's Update page does. " +
			"The first call changes nothing and returns a preview and a confirm_code; call again with the same arguments and the code to update. " +
			"A running game is warned, saved and stopped, a \"Before update\" snapshot is taken, and the game starts again on the new version. " +
			"Updating past the pre-start check's problems is offered only in Foreman's web UI." + mcpWaitNote()}, s.mcpUpdateGameVersion)
	mcp.AddTool(srv, &mcp.Tool{Name: "rollback_game_version", Annotations: twoStep,
		Description: "Put a world back on the Factorio version it had before its last update, in two steps, as the world's Roll back page does: " +
			"a \"Before restore\" snapshot is taken, then the \"Before update\" snapshot is restored. Play since the update is lost. " +
			"The first call changes nothing and returns a preview and a confirm_code; call again with the same arguments and the code to roll back." + mcpWaitNote()}, s.mcpRollbackGameVersion)
}

func mcpWaitNote() string {
	return fmt.Sprintf(" The call waits up to %d seconds; if the action is still running then, server_status reports its result.", int(mcpWait.Seconds()))
}

type mcpVersionsIn struct {
	WorldID string `json:"world_id,omitempty" jsonschema:"a world's id from list_worlds, to list only versions it can update to; every version when left out"`
}

type mcpVersionsOut struct {
	World        *mcpWorldRef `json:"world,omitempty"`
	Current      string       `json:"current_version,omitempty"`
	Versions     []string     `json:"versions"`
	Stable       string       `json:"stable,omitempty"`
	Experimental string       `json:"experimental,omitempty"`
	// RollbackTo is the version rollback_game_version would put the world
	// back on; empty when there is none.
	RollbackTo string `json:"rollback_to,omitempty"`
}

func (s *server) mcpListGameVersions(ctx context.Context, _ *mcp.CallToolRequest, in mcpVersionsIn) (*mcp.CallToolResult, mcpVersionsOut, error) {
	out := mcpVersionsOut{}
	above := ""
	if in.WorldID != "" {
		sum, err := s.mcpWorld(ctx, in.WorldID)
		if err != nil {
			return nil, mcpVersionsOut{}, err
		}
		ref := worldRef(sum)
		out.World, out.Current, above = &ref, sum.GameVersion, sum.GameVersion
		if _, version, err := s.worlds.RollbackTarget(sum.ID); err == nil {
			out.RollbackTo = version
		} else if !errors.Is(err, world.ErrNoRollback) {
			return nil, mcpVersionsOut{}, s.mcpFail("find the rollback snapshot", err)
		}
	}
	p := s.versionPicker(ctx, above)
	if p.Unreachable {
		return nil, mcpVersionsOut{}, errors.New("foreman could not reach Docker Hub to list the game versions; try again later")
	}
	out.Versions, out.Stable, out.Experimental = settingsNames(p.Versions), p.Stable, p.Experimental
	return mcpText("%d game versions.", len(out.Versions)), out, nil
}

type mcpUpdateIn struct {
	mcpConfirmIn
	WorldID          string `json:"world_id" jsonschema:"the world's id from list_worlds"`
	Version          string `json:"version" jsonschema:"the Factorio version to move to, three numbers newer than the world's, from list_game_versions"`
	CountdownSeconds *int   `json:"countdown_seconds,omitempty" jsonschema:"seconds players are warned before a running game stops, 0 to 600; the app's default when left out"`
}

type mcpVersionOut struct {
	mcpPreview
	mcpActionOut
}

// updateWill is the update page's list in English.
func updateWill(p UpdatePage) []string {
	will := []string{inEnglish(i18n.M("server.update.download"))}
	if p.Running {
		will = append(will, inEnglish(i18n.M("server.update.warn")))
	}
	will = append(will,
		inEnglish(i18n.M("server.update.snapshot", "world", p.World.Name, "label", i18n.M("worlds.snapshot.label.before_update"))),
		inEnglish(i18n.M("server.update.set")))
	if p.Running {
		will = append(will, inEnglish(i18n.M("server.update.start")))
	} else {
		will = append(will, inEnglish(i18n.M("server.anyway.leave_stopped")))
	}
	return append(will, inEnglish(i18n.M("server.update.undo")))
}

// mcpUpdateGameVersion moves a world to a newer game version in two steps
// (SPEC 21.8, 10.3). The seen state is the world, its version and target,
// and whether its game runs.
func (s *server) mcpUpdateGameVersion(ctx context.Context, req *mcp.CallToolRequest, in mcpUpdateIn) (*mcp.CallToolResult, mcpVersionOut, error) {
	sum, err := s.mcpNamed(ctx, in.WorldID)
	if err != nil {
		return nil, mcpVersionOut{}, err
	}
	stop, err := stopOptions(in.CountdownSeconds)
	if err != nil {
		return nil, mcpVersionOut{}, err
	}
	target := strings.TrimSpace(in.Version)
	p := s.updatePage(sum, target, CountdownForm{})
	if !p.Refusal.IsZero() {
		return nil, mcpVersionOut{}, errors.New(inEnglish(p.Refusal))
	}
	if err := world.CheckUpdateTarget(world.Meta{GameVersion: sum.GameVersion}, target); err != nil {
		if msg := updateTargetError(err); !msg.IsZero() {
			return nil, mcpVersionOut{}, errors.New(inEnglish(msg))
		}
		return nil, mcpVersionOut{}, s.mcpFail("check the version", err)
	}
	seen := strings.Join([]string{sum.ID, sum.GameVersion, target, strconv.FormatBool(p.Running)}, "\n")
	preview := func() (*mcp.CallToolResult, mcpVersionOut, error) {
		pv, err := s.mcpAsk(req, in, seen)
		if err != nil {
			return nil, mcpVersionOut{}, err
		}
		pv.Will, pv.Snapshot = updateWill(p), world.LabelBeforeUpdate
		if p.Running {
			pv.UntrustedOnline = s.online(ctx, sum.ID).Names
		}
		out := mcpVersionOut{mcpPreview: pv, mcpActionOut: mcpActionOut{World: worldRef(sum), Action: game.AuditUpdate}}
		return mcpText("Nothing has changed yet. Call %s again with the same arguments and confirm_code to update %s from %s to %s.",
			mcpTool(req), sum.Name, sum.GameVersion, target), out, nil
	}
	if in.ConfirmCode == "" {
		return preview()
	}
	was, err := s.mcpSpend(req, in)
	if err != nil {
		return nil, mcpVersionOut{}, err
	}
	if was.moved(seen) {
		return mcpRetryPreview(preview, "World %s changed since the preview, so nothing was updated. Here is a fresh preview and code.", sum.Name)
	}
	id, by := sum.ID, mcpActor(req)
	// Updating past the pre-start check's problems is offered only in the
	// web UI, so no "anyway" option is passed.
	res, act, err := s.mcpRun(ctx, sum, game.AuditUpdate, []string{id}, true, func(ctx context.Context) actionResult {
		r := s.finish(id, game.AuditUpdate, s.game.Update(ctx, id, target, by, stop, game.StartOptions{}))
		r.Version = target
		return r
	})
	return res, mcpVersionOut{mcpPreview: mcpPreview{Done: true}, mcpActionOut: act}, err
}

type mcpRollbackIn struct {
	mcpConfirmIn
	WorldID          string `json:"world_id" jsonschema:"the world's id from list_worlds"`
	CountdownSeconds *int   `json:"countdown_seconds,omitempty" jsonschema:"seconds players are warned before a running game stops, 0 to 600; the app's default when left out"`
}

// rollbackWill is the rollback page's list in English.
func rollbackWill(p RollbackPage) []string {
	var will []string
	if p.Running {
		will = append(will, inEnglish(i18n.M("server.update.warn")))
	}
	before := i18n.M("worlds.snapshot.label.before_restore")
	will = append(will,
		inEnglish(i18n.M("server.rollback.snapshot", "world", p.World.Name, "label", before)),
		inEnglish(i18n.M("server.rollback.restore", "label", snapshotLabelArg(p.Snapshot),
			"time", p.Snapshot.TakenAt.UTC().Format("2006-01-02 15:04 UTC"), "version", p.Version)))
	if p.Running {
		will = append(will, inEnglish(i18n.M("server.rollback.start", "version", p.Version)))
	}
	return append(will, inEnglish(i18n.M("server.rollback.undo", "label", before)))
}

// mcpRollbackGameVersion restores a world's "Before update" snapshot in two
// steps (SPEC 21.8, 10.4). The seen state is the world, the snapshot and
// version it goes back to, and whether its game runs.
func (s *server) mcpRollbackGameVersion(ctx context.Context, req *mcp.CallToolRequest, in mcpRollbackIn) (*mcp.CallToolResult, mcpVersionOut, error) {
	sum, err := s.mcpNamed(ctx, in.WorldID)
	if err != nil {
		return nil, mcpVersionOut{}, err
	}
	p, err := s.rollbackPage(sum)
	if err != nil {
		return nil, mcpVersionOut{}, s.mcpFail("find the rollback snapshot", err)
	}
	if !p.Error.IsZero() {
		return nil, mcpVersionOut{}, errors.New(inEnglish(p.Error))
	}
	stop := game.StopOptions{Countdown: game.StopCountdown}
	if p.Running {
		if stop, err = stopOptions(in.CountdownSeconds); err != nil {
			return nil, mcpVersionOut{}, err
		}
	}
	seen := strings.Join([]string{sum.ID, sum.GameVersion, p.Snapshot.Name, p.Version, strconv.FormatBool(p.Running)}, "\n")
	preview := func() (*mcp.CallToolResult, mcpVersionOut, error) {
		pv, err := s.mcpAsk(req, in, seen)
		if err != nil {
			return nil, mcpVersionOut{}, err
		}
		pv.Will, pv.Snapshot = rollbackWill(p), world.LabelBeforeRestore
		if p.Running {
			pv.UntrustedOnline = s.online(ctx, sum.ID).Names
		}
		out := mcpVersionOut{mcpPreview: pv, mcpActionOut: mcpActionOut{World: worldRef(sum), Action: game.AuditRollback}}
		return mcpText("Nothing has changed yet. Call %s again with the same arguments and confirm_code to roll %s back from %s to %s.",
			mcpTool(req), sum.Name, sum.GameVersion, p.Version), out, nil
	}
	if in.ConfirmCode == "" {
		return preview()
	}
	was, err := s.mcpSpend(req, in)
	if err != nil {
		return nil, mcpVersionOut{}, err
	}
	if was.moved(seen) {
		return mcpRetryPreview(preview, "World %s changed since the preview, so nothing was rolled back. Here is a fresh preview and code.", sum.Name)
	}
	id, by, snap := sum.ID, mcpActor(req), p.Snapshot.Name
	res, act, err := s.mcpRun(ctx, sum, game.AuditRollback, []string{id}, true, func(ctx context.Context) actionResult {
		before, err := s.game.Rollback(ctx, id, by, stop)
		r := s.finish(id, game.AuditRollback, err)
		r.Restored, r.Undo = snap, before.Name
		return r
	})
	return res, mcpVersionOut{mcpPreview: mcpPreview{Done: true}, mcpActionOut: act}, err
}
