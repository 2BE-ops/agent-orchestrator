package goose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// setIdentityRunner swaps the package-level identity-probe runner and restores
// it at the end of the test, so binary-resolution tests are hermetic: they do
// not depend on whatever `goose` (Block or pressly) happens to be installed on
// the host or reachable via the spec's hardcoded fallback paths such as
// /opt/homebrew/bin/goose. This mirrors the authprobe.CmdRunner injection
// pattern.
func setIdentityRunner(t *testing.T, fn func(ctx context.Context, name string, args ...string) ([]byte, error)) {
	t.Helper()
	orig := identityCmdRunner
	t.Cleanup(func() { identityCmdRunner = orig })
	identityCmdRunner = fn
}

const presslyMigrationHelp = `goose is a database migration tool.

Commands:
  up          Migrate the DB to the most recent version available
  down        Roll back the version by 1
  status      Dump the migration status for the current DB
  create      Create a blank migration template
  version     Print the goose version
`

const blockGooseHelp = `An AI agent

Usage: goose [COMMAND]

Commands:
  configure     Configure goose settings
  session       Start or resume interactive chat sessions
  run           Execute commands from an instruction file or stdin
  recipe        Recipe utilities for validation and deeplinking
`

// TestResolveGooseBinaryRejectsPresslyMigrationTool verifies the #4516 fix: a
// `goose` binary whose help advertises the pressly/goose DB migration commands
// (and no Block-Goose session/recipe marker) is rejected, so the resolver
// reports goose as not installed instead of trusting the name alone. Before
// the fix this returned the migration tool's path (false "installed").
func TestResolveGooseBinaryRejectsPresslyMigrationTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix PATH lookup shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "goose")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLTA_HOME", "")
	t.Setenv("FNM_DIR", "")
	// Every candidate that resolves (PATH or any hardcoded fallback) is probed
	// as the pressly migration tool, so none can validate as Block Goose.
	setIdentityRunner(t, func(context.Context, string, ...string) ([]byte, error) {
		return []byte(presslyMigrationHelp), nil
	})

	_, err := ResolveGooseBinary(context.Background())
	if !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("want ports.ErrAgentBinaryNotFound for the pressly migration tool, got err=%v", err)
	}
}

// TestResolveGooseBinaryAcceptsBlockGoose verifies the fix does not over-reject:
// a `goose` binary whose help advertises Block-Goose subcommands (session/recipe)
// is accepted and resolved.
func TestResolveGooseBinaryAcceptsBlockGoose(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix PATH lookup shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "goose")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLTA_HOME", "")
	t.Setenv("FNM_DIR", "")
	setIdentityRunner(t, func(context.Context, string, ...string) ([]byte, error) {
		return []byte(blockGooseHelp), nil
	})

	got, err := ResolveGooseBinary(context.Background())
	if err != nil {
		t.Fatalf("want the planted Block Goose binary, got err=%v", err)
	}
	if got != bin {
		t.Fatalf("ResolveGooseBinary = %q, want %q", got, bin)
	}
}

// TestResolveGooseBinaryRejectsUnrecognizableHelp verifies the probe fails
// closed: a `goose` whose help lacks both Block-Goose markers and the pressly
// signature is rejected too (not trusted by name).
func TestResolveGooseBinaryRejectsUnrecognizableHelp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix PATH lookup shape")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "goose")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("VOLTA_HOME", "")
	t.Setenv("FNM_DIR", "")
	setIdentityRunner(t, func(context.Context, string, ...string) ([]byte, error) {
		return []byte("some unrelated tool with no goose markers\n"), nil
	})

	_, err := ResolveGooseBinary(context.Background())
	if !errors.Is(err, ports.ErrAgentBinaryNotFound) {
		t.Fatalf("want ports.ErrAgentBinaryNotFound for unrecognizable help, got err=%v", err)
	}
}
