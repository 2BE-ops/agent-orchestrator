package agent

import (
	"context"
	"errors"
	"testing"

	agentregistry "github.com/aoagents/agent-orchestrator/backend/internal/adapters/agent/registry"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type configurationAgent struct{ fakeAgent }

func (configurationAgent) GetConfigSpec(context.Context) (ports.ConfigSpec, error) {
	return ports.ConfigSpec{Fields: []ports.ConfigField{{Key: "model", Type: ports.ConfigFieldString}, {Key: "permissions", Type: ports.ConfigFieldEnum, Enum: []string{"default", "auto"}}}}, nil
}

type configurationChat struct {
	supported bool
	err       error
	calls     int
}

func (c *configurationChat) SupportsChat(domain.AgentHarness) bool { return c.supported }
func (c *configurationChat) InspectCapabilities(context.Context, domain.AgentHarness) (ports.ChatCapabilities, error) {
	c.calls++
	return ports.ChatCapabilities{ports.ChatCapabilityStreaming: true}, c.err
}

func TestConfigurationRespectsAdapterFieldsAndMode(t *testing.T) {
	s := NewWithAgents([]agentregistry.HarnessAgent{{Harness: domain.HarnessCodex, Agent: configurationAgent{}}})
	chat := &configurationChat{supported: true}
	s.chatConfiguration = chat
	result, err := s.Configuration(context.Background(), "codex", domain.SessionModeTUI)
	if err != nil || len(result.Fields) != 2 || len(result.SessionModes) != 2 || chat.calls != 0 {
		t.Fatalf("TUI configuration unexpectedly probed: %+v %v", result, err)
	}
	result, err = s.Configuration(context.Background(), "codex", domain.SessionModeChat)
	if err != nil || result.CapabilityState != "supported" || len(result.ChatCapabilities) != 1 || chat.calls != 1 {
		t.Fatalf("chat configuration: %+v %v", result, err)
	}
	chat.err = errors.New("native probe failed with sensitive details")
	result, err = s.Configuration(context.Background(), "codex", domain.SessionModeChat)
	if err != nil || result.CapabilityState != "unknown" || len(result.ChatCapabilities) != 0 || result.Warning == chat.err.Error() {
		t.Fatalf("unknown capability not preserved/sanitized: %+v %v", result, err)
	}
	chat.supported = false
	result, err = s.Configuration(context.Background(), "codex", domain.SessionModeChat)
	if err != nil || result.CapabilityState != "unsupported" || len(result.SessionModes) != 1 || chat.calls != 2 {
		t.Fatalf("unsupported mode probed: %+v %v", result, err)
	}
	for _, input := range []struct {
		harness string
		mode    domain.SessionMode
	}{{"invented", domain.SessionModeTUI}, {"codex", "invented"}} {
		if _, err := s.Configuration(context.Background(), input.harness, input.mode); err == nil {
			t.Fatalf("invalid input accepted: %+v", input)
		}
	}
}
