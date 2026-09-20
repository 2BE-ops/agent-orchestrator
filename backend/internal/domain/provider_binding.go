package domain

import (
	"fmt"
	"strings"
	"time"
)

// ProviderBinding names an existing native provider configuration. It never
// carries credentials, environment values, arbitrary paths or commands.
type ProviderBinding struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Harness   AgentHarness `json:"harness"`
	Provider  string       `json:"provider"`
	ProjectID string       `json:"projectId"`
	Enabled   bool         `json:"enabled"`
	Revision  int64        `json:"revision"`
	CreatedAt time.Time    `json:"createdAt"`
	UpdatedAt time.Time    `json:"updatedAt"`
}

// ProviderBindingAudit retains each human change independently of CDC retention.
type ProviderBindingAudit struct {
	Sequence  int64
	BindingID string
	Revision  int64
	Action    string
	ActorID   string
	Reason    string
	Name      string
	Enabled   bool
	CreatedAt time.Time
}

// Validate checks the bounded reference; native catalog membership is a service
// check so an unavailable/deleted provider never silently becomes the default.
func (b ProviderBinding) Validate() error {
	if !b.Harness.IsKnown() || !registryText(b.Name, 120, true) || !registryText(b.Provider, 256, false) || !registryText(b.ProjectID, 200, false) || strings.TrimSpace(b.Provider) != b.Provider {
		return fmt.Errorf("invalid provider binding name, harness or native reference")
	}
	return nil
}
