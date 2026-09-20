package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func newTaskEvaluationCommands(ctx *commandContext) []*cobra.Command {
	commands := newTaskAttemptReadCommands(ctx, "evaluation", "evaluations", "independent assessment", "evaluation-id")
	evaluate := newTaskAttemptRequestCommand(ctx, "evaluate", "Collect independent evidence for an exact worker result", "evaluations")
	return append(commands, evaluate)
}

// Both independent review and evidence collection use the same bounded request
// transport for an exact result within a task attempt.
func newTaskAttemptRequestCommand(ctx *commandContext, verb, summary, resource string) *cobra.Command {
	var file string
	request := &cobra.Command{Use: verb + " <task-id> <attempt-id>", Short: summary, Args: usageArgs(cobra.ExactArgs(2)), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
			return usageError{errors.New("task and attempt ids must not be blank")}
		}
		body, err := readAPIRequestJSON(cmd.InOrStdin(), file, 8<<10)
		if err != nil {
			return err
		}
		var response json.RawMessage
		path := "tasks/" + url.PathEscape(args[0]) + "/attempts/" + url.PathEscape(args[1]) + "/" + resource
		if err := ctx.postJSON(cmd.Context(), path, body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	request.Flags().StringVar(&file, "file", "", "Request JSON file, or - for stdin (required)")
	return request
}
