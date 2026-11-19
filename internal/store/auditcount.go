package store

import (
	"context"
	"fmt"
	"time"
)

// CountAuditBefore counts the entries older than t, the ones a retention
// sweep would delete (SPEC 12.9).
func (s *Store) CountAuditBefore(ctx context.Context, t time.Time) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_log WHERE time < ?`, t.Unix()).Scan(&n); err != nil {
		return 0, fmt.Errorf("count old audit entries: %w", err)
	}
	return n, nil
}
