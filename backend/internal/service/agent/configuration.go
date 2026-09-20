package agent

import (
	"context"
	"sort"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apierr"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// ChatConfiguration inspects existing driver support without starting a worker.
type ChatConfiguration interface {
	SupportsChat(domain.AgentHarness) bool
	InspectCapabilities(context.Context, domain.AgentHarness) (ports.ChatCapabilities, error)
}

// ConfigurationField projects the adapter's declared configuration contract.
type ConfigurationField struct {
	Key         string   `json:"key"`
	Type        string   `json:"type"`
	Description string   `json:"description"`
	Options     []string `json:"options"`
}

// Configuration distinguishes declared support from current native availability.
type Configuration struct {
	Harness          string               `json:"harness"`
	Fields           []ConfigurationField `json:"fields"`
	SessionModes     []domain.SessionMode `json:"sessionModes"`
	ChatCapabilities []string             `json:"chatCapabilities"`
	CapabilityState  string               `json:"capabilityState" enum:"supported,unsupported,unknown"`
	Warning          string               `json:"warning,omitempty"`
}

// Configuration reads the adapter contract and, only for requested chat mode,
// the native driver's bounded capability probe. Credentials remain native.
func (s *Service) Configuration(ctx context.Context, agentID string, mode domain.SessionMode) (Configuration, error) {
	item, ok := s.agent(agentID)
	if !ok {
		return Configuration{}, apierr.Invalid("UNKNOWN_AGENT_ID", "Unknown agent adapter", nil)
	}
	if !mode.Valid() {
		return Configuration{}, apierr.Invalid("INVALID_SESSION_MODE", "Mode must be tui or chat", nil)
	}
	spec, err := item.Agent.GetConfigSpec(ctx)
	if err != nil {
		return Configuration{}, err
	}
	result := Configuration{Harness: agentID, Fields: []ConfigurationField{}, SessionModes: []domain.SessionMode{domain.SessionModeTUI}, ChatCapabilities: []string{}, CapabilityState: "supported"}
	for _, field := range spec.Fields {
		result.Fields = append(result.Fields, ConfigurationField{Key: field.Key, Type: string(field.Type), Description: field.Description, Options: append([]string{}, field.Enum...)})
	}
	if s.chatConfiguration != nil && s.chatConfiguration.SupportsChat(item.Harness) {
		result.SessionModes = append(result.SessionModes, domain.SessionModeChat)
	}
	if mode != domain.SessionModeChat {
		return result, nil
	}
	if len(result.SessionModes) == 1 {
		result.CapabilityState = "unsupported"
		result.Warning = "This harness has no Chat driver."
		return result, nil
	}
	probeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	capabilities, err := s.chatConfiguration.InspectCapabilities(probeCtx, item.Harness)
	if err != nil {
		result.CapabilityState = "unknown"
		result.Warning = "Native Chat capabilities could not be verified. Check the harness installation and authentication."
		return result, nil
	}
	for capability, supported := range capabilities {
		if supported {
			result.ChatCapabilities = append(result.ChatCapabilities, string(capability))
		}
	}
	sort.Strings(result.ChatCapabilities)
	return result, nil
}
