package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrNoWebhook is returned when no webhook has the id given.
var ErrNoWebhook = errors.New("no webhook with that id")

// Webhook is one row of the webhooks table. URL is sealed: the store never
// sees the plain address (SPEC 14.8).
type Webhook struct {
	ID        int64
	Preset    string
	Host      string
	URL       []byte
	EventsOff string
	Lang      string
	CreatedAt time.Time
	CreatedBy string
	UpdatedAt time.Time
	UpdatedBy string
}

// SealFunc seals an address for the row with the id given. The row's id is
// part of the sealed value, so it is only known once the row exists.
type SealFunc func(id int64) ([]byte, error)

// AddWebhook inserts w and seals its address in one transaction, so no row
// is ever left without one. It returns the new id; w.ID and w.URL are
// ignored.
func (s *Store) AddWebhook(ctx context.Context, w Webhook, seal SealFunc, now time.Time) (_ int64, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("add webhook: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	res, err := tx.ExecContext(ctx,
		`INSERT INTO webhooks (preset, host, url, events_off, lang, created_at, created_by, updated_at, updated_by)
		 VALUES (?, ?, x'', ?, ?, ?, ?, ?, ?)`,
		w.Preset, w.Host, w.EventsOff, w.Lang, now.Unix(), w.CreatedBy, now.Unix(), w.CreatedBy)
	if err != nil {
		return 0, fmt.Errorf("add webhook: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("add webhook: %w", err)
	}
	sealed, err := seal(id)
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE webhooks SET url = ? WHERE id = ?", sealed, id); err != nil {
		return 0, fmt.Errorf("add webhook: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return 0, fmt.Errorf("add webhook: %w", err)
	}
	return id, nil
}

// UpdateWebhook changes a webhook's events and language, and its preset,
// host and address when sealedURL is not nil. It returns ErrNoWebhook when
// the row is gone.
func (s *Store) UpdateWebhook(ctx context.Context, w Webhook, sealedURL []byte, now time.Time) error {
	var res sql.Result
	var err error
	if sealedURL != nil {
		res, err = s.db.ExecContext(ctx,
			`UPDATE webhooks SET preset = ?, host = ?, url = ?, events_off = ?, lang = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
			w.Preset, w.Host, sealedURL, w.EventsOff, w.Lang, now.Unix(), w.UpdatedBy, w.ID)
	} else {
		res, err = s.db.ExecContext(ctx,
			`UPDATE webhooks SET events_off = ?, lang = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
			w.EventsOff, w.Lang, now.Unix(), w.UpdatedBy, w.ID)
	}
	if err != nil {
		return fmt.Errorf("update webhook: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNoWebhook
	}
	return nil
}

const webhookColumns = "id, preset, host, url, events_off, lang, created_at, created_by, updated_at, updated_by"

func scanWebhook(row interface{ Scan(...any) error }) (Webhook, error) {
	var w Webhook
	var created, updated int64
	err := row.Scan(&w.ID, &w.Preset, &w.Host, &w.URL, &w.EventsOff, &w.Lang, &created, &w.CreatedBy, &updated, &w.UpdatedBy)
	w.CreatedAt, w.UpdatedAt = time.Unix(created, 0).UTC(), time.Unix(updated, 0).UTC()
	return w, err
}

// Webhook returns one webhook, or ErrNoWebhook.
func (s *Store) Webhook(ctx context.Context, id int64) (Webhook, error) {
	w, err := scanWebhook(s.db.QueryRowContext(ctx, "SELECT "+webhookColumns+" FROM webhooks WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Webhook{}, ErrNoWebhook
	}
	if err != nil {
		return Webhook{}, fmt.Errorf("read webhook: %w", err)
	}
	return w, nil
}

// ListWebhooks returns every webhook, oldest first.
func (s *Store) ListWebhooks(ctx context.Context) (_ []Webhook, err error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+webhookColumns+" FROM webhooks ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	defer func() {
		if cerr := rows.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()
	var out []Webhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, fmt.Errorf("list webhooks: %w", err)
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list webhooks: %w", err)
	}
	return out, nil
}

// DeleteWebhook removes a webhook. It returns ErrNoWebhook when there is none.
func (s *Store) DeleteWebhook(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM webhooks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete webhook: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNoWebhook
	}
	return nil
}
