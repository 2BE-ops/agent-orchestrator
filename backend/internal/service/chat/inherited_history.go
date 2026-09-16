package chat

import (
	"context"
	"maps"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

// Keep ancestor rows where they were originally recorded. Only a provider-proven
// fork with identical, complete turns may omit those copies from a new scope.
func (s *Service) withoutInheritedHistory(ctx context.Context, provider ports.ChatConversation, active domain.ConversationBranch, retained ConversationRows, events []ports.ChatEvent) ([]ports.ChatEvent, error) {
	reader, ok := provider.(ports.ChatInheritedHistory)
	if !ok {
		return events, nil
	}
	var ancestors []domain.ConversationBranch
	seenScopes := map[string]bool{}
	if active.ProviderConversationID == provider.ProviderConversationID() {
		seenScopes[active.ProviderScopeID] = true
	}
	for branch := active; ; {
		if !seenScopes[branch.ProviderScopeID] {
			ancestors = append(ancestors, branch)
			seenScopes[branch.ProviderScopeID] = true
		}
		if branch.ParentBranchID == "" {
			break
		}
		var err error
		branch, err = s.store.ConversationBranch(ctx, active.ConversationID, branch.ParentBranchID)
		if err != nil {
			return nil, err
		}
	}
	// Nested forks replay the oldest inherited prefix first.
	for i := len(ancestors) - 1; i >= 0; i-- {
		ancestor := ancestors[i]
		mapped, err := reader.InheritedHistory(ctx, ancestor, events)
		if err != nil {
			return nil, err
		}
		if len(mapped) != len(events) {
			continue
		}
		rows, err := s.nativeReplayRows(ctx, ancestor, retained)
		if err != nil {
			return nil, err
		}
		events = omitCopiedPrefix(events, mapped, rows)
	}
	return events, nil
}

func omitCopiedPrefix(events, mapped []ports.ChatEvent, rows ConversationRows) []ports.ChatEvent {
	// Snapshot presentation hides inactive providers' turn handles. The item
	// identities remain durable, so index by them with AO turn IDs as local keys.
	rows.Turns = append([]domain.ConversationTurn(nil), rows.Turns...)
	for i := range rows.Turns {
		rows.Turns[i].ProviderTurnID = rows.Turns[i].ID
	}
	index := indexNativeHistoryTurns(rows.Turns, rows.Messages, rows.Activities)
	if index == nil {
		return events
	}
	copied := make(map[string]bool)
	var candidate *nativeHistoryTurn
	messages, activities := map[string]int{}, map[string]int{}
	ambiguous := false
	for i, event := range mapped {
		if matched := index.providerItems[event.ProviderItemID]; matched != nil {
			if candidate != nil && matched != candidate {
				ambiguous = true
			}
			candidate = matched
		}
		if fingerprint, ok := nativeHistoryEventMessageFingerprint(event); ok {
			messages[fingerprint]++
		}
		if event.Kind == ports.ChatEventActivityCompleted {
			activities[nativeHistoryActivityFingerprint(event.ActivityKind, event.ActivityStatus, event.Summary, event.Detail)]++
		}
		if event.Kind != ports.ChatEventTurnCompleted {
			continue
		}
		if candidate == nil || ambiguous || candidate.state != domain.TurnStateCompleted ||
			(event.TurnState != domain.TurnStateCompleted && event.TurnState != domain.TurnStateRecovered) ||
			!maps.Equal(messages, candidate.messages) || !maps.Equal(activities, candidate.activities) {
			break
		}
		copied[events[i].ProviderTurnID] = true
		candidate, messages, activities = nil, map[string]int{}, map[string]int{}
	}
	filtered := make([]ports.ChatEvent, 0, len(events))
	for _, event := range events {
		if !copied[event.ProviderTurnID] {
			filtered = append(filtered, event)
		}
	}
	return filtered
}
