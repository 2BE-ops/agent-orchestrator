package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// Daemon-owned user preferences.
//
// The row is seeded by migration, so a read is a plain SELECT and no caller has
// to handle "settings do not exist yet".

// AppSettings is the durable preference set. Field-compatible with
// service/settings.Snapshot, which the daemon wiring adapts.
type AppSettings struct {
	// DefaultSessionMode is the interface a new session gets when the spawn does
	// not name one. Never applied to an existing session: only an explicit
	// interface transition changes a live session's committed mode, so
	// changing this only affects sessions created afterwards.
	DefaultSessionMode domain.SessionMode
	// CloudOffering is the user's cloud toggle (Settings, Developer Mode). The
	// daemon gate combines it with the deployment's control-plane URL.
	CloudOffering bool
	// MaxConcurrentWorkers caps simultaneously running worker sessions. It is
	// enforced inside the session-creation transaction, never by an LLM.
	MaxConcurrentWorkers int
	UpdatedAt            time.Time
}

// GetAppSettings reads the preference row.
func (s *Store) GetAppSettings(ctx context.Context) (AppSettings, error) {
	row, err := s.qr.GetAppSettings(ctx)
	if err != nil {
		return AppSettings{}, fmt.Errorf("read app settings: %w", err)
	}
	return AppSettings{
		// Normalized on read: a value written by a build that knows a mode this
		// one does not must still resolve to something dispatchable.
		DefaultSessionMode:   domain.NormalizeSessionMode(row.DefaultSessionMode),
		CloudOffering:        row.CloudOffering,
		MaxConcurrentWorkers: int(row.MaxConcurrentWorkers),
		UpdatedAt:            row.UpdatedAt,
	}, nil
}

// SetDefaultSessionMode persists the default interface for new sessions.
func (s *Store) SetDefaultSessionMode(ctx context.Context, mode domain.SessionMode, now time.Time) error {
	if !mode.Valid() {
		return fmt.Errorf("invalid session mode %q", mode)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.SetDefaultSessionMode(ctx, gen.SetDefaultSessionModeParams{
		DefaultSessionMode: mode,
		UpdatedAt:          now,
	}); err != nil {
		return fmt.Errorf("set default session mode: %w", err)
	}
	return nil
}

// SetCloudOffering persists the user's cloud toggle.
func (s *Store) SetCloudOffering(ctx context.Context, enabled bool, now time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.SetCloudOffering(ctx, gen.SetCloudOfferingParams{
		CloudOffering: enabled,
		UpdatedAt:     now,
	}); err != nil {
		return fmt.Errorf("set cloud offering: %w", err)
	}
	return nil
}

// SetMaxConcurrentWorkers persists the daemon-wide concurrent worker cap.
// The limit affects the next session-creation transaction; running workers
// are never terminated by lowering it.
func (s *Store) SetMaxConcurrentWorkers(ctx context.Context, limit int, now time.Time) error {
	if limit < 1 || limit > 1000 {
		return fmt.Errorf("max concurrent workers must be between 1 and 1000")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.qw.SetMaxConcurrentWorkers(ctx, gen.SetMaxConcurrentWorkersParams{
		MaxConcurrentWorkers: int64(limit),
		UpdatedAt:            now,
	}); err != nil {
		return fmt.Errorf("set max concurrent workers: %w", err)
	}
	return nil
}
