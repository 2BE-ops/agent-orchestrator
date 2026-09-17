package policyauthority

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

const generation = "7f80c8a9-ec67-4a16-a067-a444ffcc5cca"

func readRaw(t *testing.T, raw string) (ports.AgentSwitchFailureAuthoritySnapshot, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "telemetry_policy.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := New(path).ReadAgentSwitchFailureAuthority(context.Background())
	return snapshot, err
}

func TestReaderTreatsVersionOneAsConsentGivenWhileGated(t *testing.T) {
	got, err := readRaw(t, `{"schema_version":1,"events_enabled":true,"consent_generation":"`+generation+`","updated_at":"2026-08-28T10:15:30.000Z"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Present || !got.EventsEnabled || got.ConsentProductionEnabled || got.ConsentGeneration != generation {
		t.Fatalf("snapshot = %+v", got)
	}
}

func TestReaderReadsVersionTwoConsentProductionState(t *testing.T) {
	for _, want := range []bool{true, false} {
		value := "false"
		if want {
			value = "true"
		}
		got, err := readRaw(t, `{"schema_version":2,"events_enabled":true,"consent_generation":"`+generation+`","consent_production_enabled":`+value+`,"updated_at":"2026-08-28T10:15:30.000Z"}`)
		if err != nil {
			t.Fatal(err)
		}
		if !got.Present || !got.EventsEnabled || got.ConsentProductionEnabled != want {
			t.Fatalf("consent_production_enabled=%s: snapshot = %+v", value, got)
		}
	}
}

func TestReaderReadsVersionThreeFailureAuthorityAlongsideIdentity(t *testing.T) {
	got, err := readRaw(t, `{"schema_version":3,"events_enabled":true,"consent_generation":"`+generation+`","consent_production_enabled":true,"github_identity_enabled":false,"updated_at":"2026-08-28T10:15:30.000Z"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Present || !got.EventsEnabled || !got.ConsentProductionEnabled || got.ConsentGeneration != generation {
		t.Fatalf("snapshot = %+v", got)
	}
}

func readConsent(t *testing.T, raw string) (bool, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "telemetry_policy.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return New(path).ReadGithubIdentityConsent(context.Background())
}

func TestReadGithubIdentityConsentDefaultsOnWhenAbsent(t *testing.T) {
	got, err := New(filepath.Join(t.TempDir(), "telemetry_policy.json")).ReadGithubIdentityConsent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatalf("absent file: got %v, want true", got)
	}
}

func TestReadGithubIdentityConsentDefaultsOnForPreV3Records(t *testing.T) {
	cases := map[string]string{
		"version 1": `{"schema_version":1,"events_enabled":true,"consent_generation":"` + generation + `","updated_at":"2026-08-28T10:15:30.000Z"}`,
		"version 2": `{"schema_version":2,"events_enabled":true,"consent_generation":"` + generation + `","consent_production_enabled":true,"updated_at":"2026-08-28T10:15:30.000Z"}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := readConsent(t, raw)
			if err != nil {
				t.Fatal(err)
			}
			if !got {
				t.Fatalf("got %v, want true", got)
			}
		})
	}
}

func TestReadGithubIdentityConsentReadsStoredV3Value(t *testing.T) {
	for _, want := range []bool{true, false} {
		value := "false"
		if want {
			value = "true"
		}
		got, err := readConsent(t, `{"schema_version":3,"events_enabled":true,"consent_generation":"`+generation+`","consent_production_enabled":true,"github_identity_enabled":`+value+`,"updated_at":"2026-08-28T10:15:30.000Z"}`)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("github_identity_enabled=%s: got %v, want %v", value, got, want)
		}
	}
}

func TestReadGithubIdentityConsentErrorsOnUnsafeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "telemetry_policy.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":3,"events_enabled":true,"consent_generation":"`+generation+`","consent_production_enabled":true,"github_identity_enabled":true,"updated_at":"2026-08-28T10:15:30.000Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := New(path).ReadGithubIdentityConsent(context.Background()); err == nil {
		t.Fatalf("accepted world-readable file: got %v", got)
	}
}

func TestReadGithubIdentityConsentErrorsOnMalformedFile(t *testing.T) {
	if got, err := readConsent(t, `{"schema_version":3,"events_enabled":true,"consent_generation":"`+generation+`","consent_production_enabled":true,"updated_at":"2026-08-28T10:15:30.000Z"}`); err == nil {
		t.Fatalf("accepted malformed v3 file: got %v", got)
	}
}

func TestReaderRejectsRecordsWhoseShapeDoesNotMatchTheirVersion(t *testing.T) {
	for name, raw := range map[string]string{
		"version 2 without gate state": `{"schema_version":2,"events_enabled":true,"consent_generation":"` + generation + `","updated_at":"2026-08-28T10:15:30.000Z"}`,
		"version 1 with gate state":    `{"schema_version":1,"events_enabled":true,"consent_generation":"` + generation + `","consent_production_enabled":true,"updated_at":"2026-08-28T10:15:30.000Z"}`,
		"version 2 null gate state":    `{"schema_version":2,"events_enabled":true,"consent_generation":"` + generation + `","consent_production_enabled":null,"updated_at":"2026-08-28T10:15:30.000Z"}`,
		"version 2 string gate state":  `{"schema_version":2,"events_enabled":true,"consent_generation":"` + generation + `","consent_production_enabled":"yes","updated_at":"2026-08-28T10:15:30.000Z"}`,
		"unknown version":              `{"schema_version":4,"events_enabled":true,"consent_generation":"` + generation + `","consent_production_enabled":true,"updated_at":"2026-08-28T10:15:30.000Z"}`,
		"version 3 without identity":    `{"schema_version":3,"events_enabled":true,"consent_generation":"` + generation + `","consent_production_enabled":true,"updated_at":"2026-08-28T10:15:30.000Z"}`,
		"version 3 null identity":       `{"schema_version":3,"events_enabled":true,"consent_generation":"` + generation + `","consent_production_enabled":true,"github_identity_enabled":null,"updated_at":"2026-08-28T10:15:30.000Z"}`,
		"version 3 string identity":     `{"schema_version":3,"events_enabled":true,"consent_generation":"` + generation + `","consent_production_enabled":true,"github_identity_enabled":"yes","updated_at":"2026-08-28T10:15:30.000Z"}`,
		"version 1 with identity":       `{"schema_version":1,"events_enabled":true,"consent_generation":"` + generation + `","github_identity_enabled":true,"updated_at":"2026-08-28T10:15:30.000Z"}`,
		"version 2 with identity":       `{"schema_version":2,"events_enabled":true,"consent_generation":"` + generation + `","consent_production_enabled":true,"github_identity_enabled":true,"updated_at":"2026-08-28T10:15:30.000Z"}`,
		"unknown extra key":            `{"schema_version":2,"events_enabled":true,"consent_generation":"` + generation + `","consent_production_enabled":true,"updated_at":"2026-08-28T10:15:30.000Z","extra":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := readRaw(t, raw); err == nil {
				t.Fatalf("accepted %s: %+v", name, got)
			}
		})
	}
}
