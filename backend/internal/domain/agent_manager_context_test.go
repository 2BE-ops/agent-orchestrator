package domain

import (
	"testing"
	"time"
)

func managerContextFixture(t *testing.T) AgentManagerContext {
	t.Helper()
	hash := ContextTextHash("pinned")
	c := AgentManagerContext{ID: "context", SchemaVersion: 1, RequestID: "request", Number: 1, ControllerID: "controller", SessionID: "session", NativeGeneration: "generation", ConfigurationHash: hash, RequestHash: hash, MaxContextClass: ContextMission, Classification: ContextEngagement, EngagementID: "client-a", Policy: DefaultAgentManagerPolicy(), CreatedAt: time.Now().UTC()}
	for _, kind := range []string{"task", "criteria"} {
		c.Sources = append(c.Sources, ContextSource{Kind: kind, ID: "task", Version: 1, SourceHash: hash, Content: "sealed input", ContentHash: ContextTextHash("sealed input"), Classification: ContextEngagement, EngagementID: "client-a", Disposition: "inline", Reason: "Frozen revision"})
	}
	c.Prompt, _ = c.RenderPrompt()
	c.ContentHash = c.Hash()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestAgentManagerContextRejectsForgedAndUnboundedReceipts(t *testing.T) {
	for _, name := range []string{"higher class", "foreign engagement", "lower aggregate", "missing labels", "changed content", "changed prompt", "missing criteria", "invalid policy", "version bound", "broken chain"} {
		t.Run(name, func(t *testing.T) {
			c := managerContextFixture(t)
			switch name {
			case "higher class":
				c.MaxContextClass = ContextTechnical
			case "foreign engagement":
				c.Sources[0].EngagementID = "client-b"
			case "lower aggregate":
				c.Classification = ContextTechnical
			case "missing labels":
				c.Sources[0].Classification = ""
			case "changed content":
				c.Sources[0].Content += " changed"
			case "changed prompt":
				c.Prompt += " changed"
			case "missing criteria":
				c.Sources = c.Sources[:1]
			case "invalid policy":
				c.Policy.MaxProposalAttempts = 6
			case "version bound":
				c.Number = 33
			case "broken chain":
				c.PreviousContextHash = "invalid"
			}
			if name != "changed prompt" {
				c.Prompt, _ = c.RenderPrompt()
			}
			c.ContentHash = c.Hash()
			if err := c.Validate(); err == nil {
				t.Fatal("self-consistent hash bypassed receipt validation")
			}
		})
	}
}
