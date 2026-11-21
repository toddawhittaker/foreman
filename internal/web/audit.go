package web

import (
	"context"

	"github.com/toddawhittaker/foreman/internal/game"
	"github.com/toddawhittaker/foreman/internal/store"
)

// audit records an action that is already done, with the token's name when
// it came through one (SPEC 12.9). A failed write is logged: the action has
// happened, and undoing it would lose more than the entry.
func (s *server) audit(ctx context.Context, by game.Actor, action, worldID, detail string) {
	if err := s.addAudit(ctx, by, action, worldID, detail); err != nil {
		s.log.Error("could not record the action in the audit log", "action", action, "world", worldID, "error", err)
	}
}

// addAudit records an action and returns the write's error, for an action
// that must not happen unless its record does (SPEC 12.9).
func (s *server) addAudit(ctx context.Context, by game.Actor, action, worldID, detail string) error {
	return s.store.AddAudit(ctx, store.AuditEntry{
		Time: s.now(), UserID: by.UserID, Username: by.Username, TokenName: by.Token,
		Action: action, WorldID: worldID, Detail: detail,
	})
}
