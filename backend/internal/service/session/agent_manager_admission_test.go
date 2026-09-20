package session

import (
	"context"
	"errors"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

func TestGenericSessionSpawnRequiresAgentManagerAdmission(t *testing.T) {
	s := NewWithDeps(Deps{})
	_, _, _, err := s.Spawn(context.Background(), ports.SpawnConfig{Kind: domain.KindAgentManager, ProjectID: "project"})
	var apiError *apierr.Error
	if !errors.As(err, &apiError) || apiError.Code != "AGENT_MANAGER_ADMISSION_REQUIRED" {
		t.Fatalf("generic spawn reached native machinery: %v", err)
	}
}
