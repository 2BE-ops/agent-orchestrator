package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"github.com/spf13/cobra"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// newProjectControlCommands mounts deterministic project controls, the Needs
// Human inbox and dry-run rehearsals directly under `ao project`.
func newProjectControlCommands(ctx *commandContext) []*cobra.Command {
	read := &cobra.Command{Use: "control <project>", Short: "Inspect the project's adaptive control state (JSON output)", Args: usageArgs(cobra.ExactArgs(1))}
	read.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := controlProjectPath(args[0])
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path+"/control", &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	commands := []*cobra.Command{read}

	for _, state := range []struct {
		use, state, short string
	}{
		{"pause <project>", "paused", "Fence new admissions; current tasks continue"},
		{"resume <project>", "running", "Reopen admissions after a pause, drain or stop"},
		{"drain <project>", "draining", "Fence admissions and let current attempts finish"},
		{"stop <project>", "stopped", "Fence admissions and follow-up dispatch"},
	} {
		command := state
		cmd := &cobra.Command{Use: command.use, Short: command.short, Args: usageArgs(cobra.ExactArgs(1))}
		var reason string
		cmd.Flags().StringVar(&reason, "reason", "", "Audited control reason (required)")
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(reason) == "" {
				return usageError{errors.New("--reason is required")}
			}
			path, err := controlProjectPath(args[0])
			if err != nil {
				return err
			}
			body, err := json.Marshal(domain.ProjectControl{State: domain.ProjectControlState(command.state), Reason: reason})
			if err != nil {
				return err
			}
			var response json.RawMessage
			if err := ctx.postJSON(cmd.Context(), path+"/control", json.RawMessage(body), &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}
		commands = append(commands, cmd)
	}

	cancel := &cobra.Command{Use: "cancel <project>", Short: "Cancel pending or all work deterministically", Args: usageArgs(cobra.ExactArgs(1))}
	var cancelScope, cancelReason string
	cancel.Flags().StringVar(&cancelScope, "scope", "pending", "pending cancels unleased work; all additionally marks leased work cancelling")
	cancel.Flags().StringVar(&cancelReason, "reason", "", "Audited cancellation reason (required)")
	cancel.RunE = func(cmd *cobra.Command, args []string) error {
		if cancelScope != "pending" && cancelScope != "all" {
			return usageError{errors.New("--scope must be pending or all")}
		}
		if strings.TrimSpace(cancelReason) == "" {
			return usageError{errors.New("--reason is required")}
		}
		path, err := controlProjectPath(args[0])
		if err != nil {
			return err
		}
		body, err := json.Marshal(map[string]string{"scope": cancelScope, "reason": cancelReason})
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), path+"/control/cancel-work", json.RawMessage(body), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	commands = append(commands, cancel)

	needs := &cobra.Command{Use: "needs-human <project>", Short: "List the project's open requests for human input", Args: usageArgs(cobra.ExactArgs(1))}
	var needsAfter string
	var needsLimit int
	needs.Flags().StringVar(&needsAfter, "after", "", "Continue after this request id")
	needs.Flags().IntVar(&needsLimit, "limit", 20, "Page size (1 to 100)")
	needs.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := controlProjectPath(args[0])
		if err != nil {
			return err
		}
		if needsLimit < 1 || needsLimit > 100 {
			return usageError{errors.New("--limit must be between 1 and 100")}
		}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), fmt.Sprintf("%s/needs-human?afterId=%s&limit=%d", path, url.QueryEscape(needsAfter), needsLimit), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	commands = append(commands, needs)

	dryRun := &cobra.Command{Use: "dry-run <project>", Short: "Rehearse an autonomous plan with writes and launches disabled", Args: usageArgs(cobra.ExactArgs(1))}
	var dryRunFile string
	dryRun.Flags().StringVar(&dryRunFile, "file", "", "Plan JSON with the exact actions the orchestrator protocol submits (use - for stdin; required)")
	dryRun.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := controlProjectPath(args[0])
		if err != nil {
			return err
		}
		body, err := readAPIRequestJSON(cmd.InOrStdin(), dryRunFile, 288<<10)
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), path+"/dry-run", body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	commands = append(commands, dryRun)
	return commands
}

// newTaskNeedsHumanCommands mounts task-scoped Needs Human raise/resolve.
func newTaskNeedsHumanCommands(ctx *commandContext) []*cobra.Command {
	raise := &cobra.Command{Use: "needs-human <task-id>", Short: "Record a structured request for human input", Args: usageArgs(cobra.ExactArgs(1))}
	var raiseCode, raiseDetail string
	raise.Flags().StringVar(&raiseCode, "code", "", "Reason code: credential_missing, approval_required, ambiguous_intent, provider_unavailable or recovery_inconclusive (required)")
	raise.Flags().StringVar(&raiseDetail, "detail", "", "What the human must decide or provide (required)")
	raise.RunE = func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(raiseCode) == "" || strings.TrimSpace(raiseDetail) == "" {
			return usageError{errors.New("--code and --detail are required")}
		}
		path, err := controlTaskPath(args[0])
		if err != nil {
			return err
		}
		body, err := json.Marshal(map[string]string{"reasonCode": raiseCode, "detail": raiseDetail})
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), path+"/needs-human", json.RawMessage(body), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}

	resolve := &cobra.Command{Use: "resolve-human <task-id>", Short: "Close the pending request for human input", Args: usageArgs(cobra.ExactArgs(1))}
	var resolveText string
	resolve.Flags().StringVar(&resolveText, "resolution", "", "The recorded human decision (required)")
	resolve.RunE = func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(resolveText) == "" {
			return usageError{errors.New("--resolution is required")}
		}
		path, err := controlTaskPath(args[0])
		if err != nil {
			return err
		}
		body, err := json.Marshal(map[string]string{"resolution": resolveText})
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), path+"/needs-human/resolve", json.RawMessage(body), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	return []*cobra.Command{raise, resolve}
}

func controlProjectPath(project string) (string, error) {
	if strings.TrimSpace(project) == "" || len(project) > 200 || strings.IndexFunc(project, unicode.IsControl) >= 0 {
		return "", usageError{errors.New("project must be a non-empty ID up to 200 bytes without control characters")}
	}
	return "projects/" + url.PathEscape(project), nil
}

func controlTaskPath(taskID string) (string, error) {
	if strings.TrimSpace(taskID) == "" || len(taskID) > 200 || strings.IndexFunc(taskID, unicode.IsControl) >= 0 {
		return "", usageError{errors.New("task id must be a non-empty ID up to 200 bytes without control characters")}
	}
	return "tasks/" + url.PathEscape(taskID), nil
}
