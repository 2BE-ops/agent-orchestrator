package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func newTaskReviewCommands(ctx *commandContext) []*cobra.Command {
	request := newTaskAttemptRequestCommand(ctx, "request-review", "Request native review of a result against its frozen policy", "reviews")
	commands := make([]*cobra.Command, 0, 3)
	commands = append(commands, request)
	for _, spec := range []struct {
		use, short string
		list       bool
	}{
		{"reviews <task-id> <attempt-id> <result-id>", "List native review passes for an exact result", true},
		{"review <task-id> <attempt-id> <run-id>", "Inspect a native review and its sealed context", false},
	} {
		commands = append(commands, &cobra.Command{Use: spec.use, Short: spec.short, Args: usageArgs(cobra.ExactArgs(3)), RunE: func(cmd *cobra.Command, args []string) error {
			for _, value := range args {
				if strings.TrimSpace(value) == "" {
					return usageError{errors.New("task, attempt and result/run ids must not be blank")}
				}
			}
			path := "tasks/" + url.PathEscape(args[0]) + "/attempts/" + url.PathEscape(args[1])
			if spec.list {
				path += "/results/" + url.PathEscape(args[2]) + "/reviews"
			} else {
				path += "/reviews/" + url.PathEscape(args[2])
			}
			var response json.RawMessage
			if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}})
	}
	return commands
}
