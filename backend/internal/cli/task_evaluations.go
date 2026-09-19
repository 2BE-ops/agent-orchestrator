package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func newTaskEvaluationCommands(ctx *commandContext) []*cobra.Command {
	commands := newTaskAttemptReadCommands(ctx, "evaluation", "evaluations", "independent assessment")
	var file string
	evaluate := &cobra.Command{Use: "evaluate <task-id> <attempt-id>", Short: "Collect stored independent evidence for an exact worker result", Args: usageArgs(cobra.ExactArgs(2)), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
			return usageError{errors.New("task and attempt ids must not be blank")}
		}
		body, err := readAPIRequestJSON(cmd.InOrStdin(), file, 8<<10)
		if err != nil {
			return err
		}
		var response json.RawMessage
		path := "tasks/" + url.PathEscape(args[0]) + "/attempts/" + url.PathEscape(args[1]) + "/evaluations"
		if err := ctx.postJSON(cmd.Context(), path, body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	evaluate.Flags().StringVar(&file, "file", "", "Evaluation request JSON file, or - for stdin (required)")
	return append(commands, evaluate)
}
