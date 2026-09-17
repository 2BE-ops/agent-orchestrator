// Package policyauthority reads the desktop-owned telemetry policy file.
// Filesystem trust checks stay in this adapter; policy coordination consumes
// only the provider-neutral authority snapshot exposed by ports.
package policyauthority

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Reader reads one fixed telemetry-policy authority path.
type Reader struct{ path string }

// New constructs an authority reader for path.
func New(path string) *Reader { return &Reader{path: path} }

// ReadAgentSwitchFailureAuthority validates and reads the durable authority.
func (r *Reader) ReadAgentSwitchFailureAuthority(ctx context.Context) (ports.AgentSwitchFailureAuthoritySnapshot, error) {
	record, present, err := r.readRecord(ctx)
	if err != nil {
		return ports.AgentSwitchFailureAuthoritySnapshot{}, err
	}
	if !present {
		return ports.AgentSwitchFailureAuthoritySnapshot{}, nil
	}
	return ports.AgentSwitchFailureAuthoritySnapshot{
		Present: true, EventsEnabled: record.EventsEnabled, ConsentGeneration: record.ConsentGeneration,
		ConsentProductionEnabled: record.ConsentProductionEnabled != nil && *record.ConsentProductionEnabled,
	}, nil
}

// ReadGithubIdentityConsent reports whether the operator opted in to sending
// their GitHub handle with product telemetry. This opt-in defaults ON: an absent
// policy file and pre-v3 records (which predate the toggle) both read as enabled;
// only an explicit v3 `github_identity_enabled: false` disables it. Unsafe or
// malformed files return an error so the caller degrades to anonymous.
func (r *Reader) ReadGithubIdentityConsent(ctx context.Context) (bool, error) {
	record, present, err := r.readRecord(ctx)
	if err != nil {
		return false, err
	}
	if !present {
		return true, nil
	}
	if record.SchemaVersion == 3 {
		return record.GithubIdentityEnabled != nil && *record.GithubIdentityEnabled, nil
	}
	return true, nil
}

// readRecord performs the shared filesystem-trust and shape validation. A nil
// error with present=false means the authority file is absent.
func (r *Reader) readRecord(ctx context.Context) (diskRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return diskRecord{}, false, err
	}
	info, err := os.Lstat(r.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return diskRecord{}, false, nil
		}
		return diskRecord{}, false, fmt.Errorf("inspect telemetry policy authority: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return diskRecord{}, false, errors.New("telemetry policy authority is unsafe")
	}
	file, err := os.Open(r.path)
	if err != nil {
		return diskRecord{}, false, fmt.Errorf("open telemetry policy authority: %w", err)
	}
	defer func() { _ = file.Close() }()
	decoder := json.NewDecoder(io.LimitReader(file, 4097))
	decoder.DisallowUnknownFields()
	var keys map[string]json.RawMessage
	if err := decoder.Decode(&keys); err != nil || (len(keys) != 4 && len(keys) != 5 && len(keys) != 6) || keys["schema_version"] == nil || keys["events_enabled"] == nil || keys["consent_generation"] == nil || keys["updated_at"] == nil {
		return diskRecord{}, false, errors.New("telemetry policy authority is malformed")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return diskRecord{}, false, errors.New("telemetry policy authority has trailing data")
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		return diskRecord{}, false, fmt.Errorf("normalize telemetry policy authority: %w", err)
	}
	decoder = json.NewDecoder(io.LimitReader(bytes.NewReader(raw), 4097))
	decoder.DisallowUnknownFields()
	var record diskRecord
	if err := decoder.Decode(&record); err != nil || !record.versionShapeValid() || uuid.Validate(record.ConsentGeneration) != nil || !validTimestamp(record.UpdatedAt) {
		return diskRecord{}, false, errors.New("telemetry policy authority fields are invalid")
	}
	return record, true, nil
}

type diskRecord struct {
	SchemaVersion            int    `json:"schema_version"`
	EventsEnabled            bool   `json:"events_enabled"`
	ConsentGeneration        string `json:"consent_generation"`
	ConsentProductionEnabled *bool  `json:"consent_production_enabled"`
	GithubIdentityEnabled    *bool  `json:"github_identity_enabled"`
	UpdatedAt                string `json:"updated_at"`
}

func (r diskRecord) versionShapeValid() bool {
	switch r.SchemaVersion {
	case 1:
		return r.ConsentProductionEnabled == nil && r.GithubIdentityEnabled == nil
	case 2:
		return r.ConsentProductionEnabled != nil && r.GithubIdentityEnabled == nil
	case 3:
		return r.ConsentProductionEnabled != nil && r.GithubIdentityEnabled != nil
	default:
		return false
	}
}

func validTimestamp(value string) bool {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && parsed.Location() == time.UTC
}

var _ ports.AgentSwitchFailureAuthorityReader = (*Reader)(nil)
