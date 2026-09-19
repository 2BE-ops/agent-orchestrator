package store_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestContextKnowledgeSelectionIsAcceptedRelevantRankedAndBounded(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	seedProject(t, s, "project")
	seedProject(t, s, "foreign")
	definition := taskDefinition()
	definition.Category = "backend"
	createTask(t, s, "task", definition)
	_, lease := reserveTask(t, s, "selection-attempt", "task")
	rec, config := workerSnapshot(t, s)
	rec.ProjectID = "project"
	if _, _, err := s.CreateTaskWorkerSession(ctx, lease.TaskLeaseToken, rec, config, lease.HeartbeatAt); err != nil {
		t.Fatal(err)
	}
	createTask(t, s, "unrelated", taskDefinition())
	for _, tc := range []struct {
		id, status, project string
		pinned              bool
		tasks, tags         []string
	}{
		{"a-general", "accepted", "project", false, nil, nil},
		{"z-pinned", "accepted", "project", true, nil, []string{"unrelated"}},
		{"b-category", "accepted", "project", false, nil, []string{"backend"}},
		{"y-task", "accepted", "project", false, []string{"task"}, nil},
		{"candidate", "candidate", "project", false, nil, nil},
		{"invalidated", "invalidated", "project", false, nil, nil},
		{"deleted", "deleted", "project", false, nil, nil},
		{"foreign", "accepted", "foreign", true, nil, nil},
		{"other-task", "accepted", "project", false, []string{"unrelated"}, nil},
		{"other-category", "accepted", "project", false, nil, []string{"frontend"}},
	} {
		d := knowledgeDefinition()
		d.Status, d.Pinned, d.TaskIDs, d.Tags = tc.status, tc.pinned, tc.tasks, tc.tags
		if _, err := s.CreateProjectKnowledge(ctx, tc.id, domain.ProjectID(tc.project), d, knowledgeMutation(0)); err != nil {
			t.Fatalf("%s: %v", tc.id, err)
		}
	}
	versions, err := s.SelectContextKnowledge(ctx, lease.AttemptID, 33)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, version := range versions {
		ids = append(ids, version.KnowledgeID)
	}
	if !reflect.DeepEqual(ids, []string{"z-pinned", "y-task", "b-category", "a-general"}) {
		t.Fatalf("selection order/scope: %v", ids)
	}
	d := knowledgeDefinition()
	d.Status = "accepted"
	d.Content = "New accepted revision"
	if _, err := s.ReviseProjectKnowledge(ctx, "a-general", d, knowledgeMutation(1)); err != nil {
		t.Fatal(err)
	}
	versions, err = s.SelectContextKnowledge(ctx, lease.AttemptID, 33)
	if err != nil || len(versions) != 4 || versions[3].Number != 2 || versions[3].Definition.Content != d.Content {
		t.Fatalf("current accepted content: %+v %v", versions, err)
	}
	for i := 0; i < 40; i++ {
		if _, err := s.CreateProjectKnowledge(ctx, fmt.Sprintf("extra-%02d", i), "project", d, knowledgeMutation(0)); err != nil {
			t.Fatal(err)
		}
	}
	versions, err = s.SelectContextKnowledge(ctx, lease.AttemptID, 33)
	if err != nil || len(versions) != 33 {
		t.Fatalf("bounded query: %d %v", len(versions), err)
	}
	if _, err := s.SelectContextKnowledge(ctx, lease.AttemptID, 34); err == nil {
		t.Fatal("unbounded context query accepted")
	}
}
