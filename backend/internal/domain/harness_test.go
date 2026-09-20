package domain

import (
	"os"
	"testing"
)

func TestPrimeAgentHarnessIsKnown(t *testing.T) {
	if HarnessPrimeAgent != AgentHarness("prime-agent") {
		t.Fatalf("HarnessPrimeAgent = %q, want prime-agent", HarnessPrimeAgent)
	}
	if !HarnessPrimeAgent.IsKnown() {
		t.Fatal("HarnessPrimeAgent.IsKnown() = false, want true")
	}
	found := false
	for _, harness := range AllHarnesses {
		if harness == HarnessPrimeAgent {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("AllHarnesses does not contain HarnessPrimeAgent")
	}
}
func TestOMPHarnessIsKnown(t *testing.T) {
	if HarnessOMP != AgentHarness("omp") {
		t.Fatalf("HarnessOMP = %q, want omp", HarnessOMP)
	}
	if !HarnessOMP.IsKnown() {
		t.Fatal("HarnessOMP.IsKnown() = false, want true")
	}
	found := false
	for _, harness := range AllHarnesses {
		if harness == HarnessOMP {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("AllHarnesses does not contain HarnessOMP")
	}
}

// TestFakeHarnessExcludedFromAllHarnesses pins the enumeration half of the
// gate: even under the opt-in, the fake harness never appears in
// AllHarnesses, so user-facing harness listings stay byte-identical.
func TestFakeHarnessExcludedFromAllHarnesses(t *testing.T) {
	t.Setenv("AO_FAKE_HARNESS", "1")
	for _, harness := range AllHarnesses {
		if harness == HarnessFake {
			t.Fatal("AllHarnesses contains HarnessFake; it must stay unlisted")
		}
	}
}

// TestFakeHarnessKnownOnlyUnderOptIn pins the validation half: IsKnown
// accepts the fake harness exactly when AO_FAKE_HARNESS carries a truthy
// token, matching the agent adapter registry's gate.
func TestFakeHarnessKnownOnlyUnderOptIn(t *testing.T) {
	restore := func(t *testing.T) {
		t.Helper()
		os.Unsetenv("AO_FAKE_HARNESS") //nolint:errcheck // test-local env hygiene
	}
	t.Run("default refuses", func(t *testing.T) {
		restore(t)
		if HarnessFake.IsKnown() {
			t.Fatal("HarnessFake.IsKnown() = true without the opt-in, want false")
		}
	})
	for _, token := range []string{"1", "true", "yes", "on"} {
		t.Run("accepts "+token, func(t *testing.T) {
			t.Setenv("AO_FAKE_HARNESS", token)
			if !HarnessFake.IsKnown() {
				t.Fatalf("HarnessFake.IsKnown() = false under AO_FAKE_HARNESS=%s, want true", token)
			}
		})
	}
	for _, token := range []string{"", "0", "false", "no", "garbage"} {
		t.Run("refuses "+token, func(t *testing.T) {
			t.Setenv("AO_FAKE_HARNESS", token)
			if HarnessFake.IsKnown() {
				t.Fatalf("HarnessFake.IsKnown() = true under AO_FAKE_HARNESS=%q, want false", token)
			}
		})
	}
}
