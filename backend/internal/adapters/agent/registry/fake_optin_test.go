package registry

import (
	"os"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/fake"
)

// constructorIDs returns the manifest ids Constructors() produces, in order.
func constructorIDs(t *testing.T) []string {
	t.Helper()
	ids := make([]string, 0, len(Constructors()))
	for _, a := range Constructors() {
		ids = append(ids, a.Manifest().ID)
	}
	return ids
}

// TestConstructorsOmitFakeByDefault proves the opt-in gate's negative half: a
// daemon started without AO_FAKE_HARNESS never constructs the fake adapter, so
// readiness inventories, launch catalogs, and any adapter-count expectations on
// a normal machine stay byte-identical to a tree without this feature.
func TestConstructorsOmitFakeByDefault(t *testing.T) {
	old, had := os.LookupEnv(fake.HarnessEnv)
	if err := os.Unsetenv(fake.HarnessEnv); err != nil {
		t.Fatalf("unset %s: %v", fake.HarnessEnv, err)
	}
	t.Cleanup(func() {
		if !had {
			return
		}
		if err := os.Setenv(fake.HarnessEnv, old); err != nil {
			t.Fatalf("restore %s: %v", fake.HarnessEnv, err)
		}
	})

	for _, id := range constructorIDs(t) {
		if id == "fake" {
			t.Fatalf("Constructors() registered the fake harness without %s set", fake.HarnessEnv)
		}
	}
}

// TestConstructorsIncludeFakeWhenOptedIn proves the positive half: the explicit
// env gate adds exactly one fake adapter, after the always-shipped set, and
// Harnessed() pairs it with the harness id a session launch would carry.
func TestConstructorsIncludeFakeWhenOptedIn(t *testing.T) {
	for _, token := range []string{"1", "true", "yes", "on"} {
		t.Run("token="+token, func(t *testing.T) {
			t.Setenv(fake.HarnessEnv, token)
			ids := constructorIDs(t)
			count := 0
			for _, id := range ids {
				if id == "fake" {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("Constructors() registered %d fake adapters under %s=%s, want exactly 1", count, fake.HarnessEnv, token)
			}
			if got := ids[len(ids)-1]; got != "fake" {
				t.Fatalf("fake harness registered at position %q, want it appended last for stable ordering", got)
			}
			found := false
			for _, ha := range Harnessed() {
				if string(ha.Harness) == "fake" {
					found = true
				}
			}
			if !found {
				t.Fatalf("Harnessed() omits the opted-in fake harness; session launch could not resolve it")
			}
		})
	}
}

// TestConstructorsRefuseNonTruthyOptIn keeps the gate honest: values like "0",
// "false", or garbage do not leak the fake harness into a user's inventory.
func TestConstructorsRefuseNonTruthyOptIn(t *testing.T) {
	for _, token := range []string{"", "0", "false", "no", "garbage"} {
		t.Run("token="+token, func(t *testing.T) {
			t.Setenv(fake.HarnessEnv, token)
			for _, id := range constructorIDs(t) {
				if id == "fake" {
					t.Fatalf("Constructors() registered the fake harness under %s=%q", fake.HarnessEnv, token)
				}
			}
		})
	}
}
