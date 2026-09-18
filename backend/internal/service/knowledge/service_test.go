package knowledge_test

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	knowledgesvc "github.com/aoagents/agent-orchestrator/backend/internal/service/knowledge"
)

type failingStore struct {
	knowledgesvc.Store
	err error
}

func (s failingStore) GetProjectKnowledge(context.Context, string) (domain.ProjectKnowledge, error) {
	return domain.ProjectKnowledge{}, s.err
}

func TestKnowledgeServicePreservesOperationalErrorsAndMapsMissingHistory(t *testing.T) {
	failure := errors.New("database unavailable")
	for _, tc := range []struct {
		err      error
		notFound bool
	}{{failure, false}, {ports.ErrKnowledgeNotFound, true}} {
		svc := knowledgesvc.New(failingStore{err: tc.err})
		_, err := svc.Get(context.Background(), "fact")
		if tc.notFound {
			var problem *apierr.Error
			if !errors.As(err, &problem) || problem.Kind != apierr.KindNotFound {
				t.Fatalf("missing mapping: %v", err)
			}
		} else if !errors.Is(err, failure) {
			t.Fatalf("operational error hidden: %v", err)
		}
	}
}
