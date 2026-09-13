package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestNativeCheckpointEvidenceRoundTripCASAndEpoch(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)
	seedProject(t, st, "native-proof")
	rec := sampleRecord("native-proof")
	rec.Mode = domain.SessionModeTUI
	rec.Metadata.RuntimeLaunchID = "launch"
	rec.Metadata.NativeCheckpointEvidence = domain.AppendNativeCheckpoint("", "native", domain.NativeCheckpointObservation{
		Generation: "launch", PromptID: "A", Submission: true, Text: "continue", SubmissionID: "submission-A",
	})
	created, err := st.CreateSession(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	before, found, err := st.GetSession(ctx, created.ID)
	if err != nil || !found || before.Metadata.NativeCheckpointEvidence != rec.Metadata.NativeCheckpointEvidence {
		t.Fatalf("roundtrip: found=%v error=%v metadata=%+v", found, err, before.Metadata)
	}
	next := before
	next.Metadata.NativeCheckpointEvidence = domain.AppendNativeCheckpoint(before.Metadata.NativeCheckpointEvidence, "native", domain.NativeCheckpointObservation{
		Generation: "launch", PromptID: "B", Text: "Done",
	})
	applied, err := st.UpdateSessionFromActivitySignal(ctx, next, before.Revision)
	if err != nil || !applied {
		t.Fatalf("activity CAS: %v %v", applied, err)
	}
	applied, err = st.UpdateSessionFromActivitySignal(ctx, before, before.Revision)
	if err != nil || applied {
		t.Fatalf("stale CAS: %v %v", applied, err)
	}
	changed, err := st.CommitSessionControllerEpoch(ctx, created.ID, domain.SessionModeTUI, domain.SessionModeChat, "native", time.Now())
	if err != nil || !changed {
		t.Fatalf("epoch: %v %v", changed, err)
	}
	after, _, err := st.GetSession(ctx, created.ID)
	if err != nil || after.Metadata.NativeCheckpointEvidence != next.Metadata.NativeCheckpointEvidence {
		t.Fatalf("handoff lost native observations: %+v %v", after.Metadata, err)
	}
}
