package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newTaskResultCommands(ctx *commandContext) []*cobra.Command {
	commands := newTaskAttemptReadCommands(ctx, "result", "results", "worker claim", "result-id")
	var file string
	submit := &cobra.Command{Use: "submit-result <session-id>", Short: "Submit generation-fenced worker claims, not verified completion", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(args[0]) == "" {
			return usageError{errors.New("worker session id must not be blank")}
		}
		body, err := readAPIRequestJSON(cmd.InOrStdin(), file, 512<<10)
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), "sessions/"+url.PathEscape(args[0])+"/task-results", body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	submit.Flags().StringVar(&file, "file", "", "Result request JSON file, or - for stdin (required)")
	return append(commands, submit)
}

// Results and evaluations share scoped, immutable version pagination.
func newTaskAttemptReadCommands(ctx *commandContext, singular, plural, description, exactArg string) []*cobra.Command {
	commands := make([]*cobra.Command, 0, 2)
	for _, exact := range []bool{false, true} {
		use, short, count := plural+" <task-id> <attempt-id>", "List immutable "+description+" history (JSON output)", 2
		if exact {
			use, short, count = singular+" <task-id> <attempt-id> <"+exactArg+">", "Inspect an exact "+description+" (JSON output)", 3
		}
		var cursor string
		var limit int
		cmd := &cobra.Command{Use: use, Short: short, Args: usageArgs(cobra.ExactArgs(count)), RunE: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if strings.TrimSpace(arg) == "" {
					return usageError{errors.New("task, attempt and record ids must not be blank")}
				}
			}
			path := "tasks/" + url.PathEscape(args[0]) + "/attempts/" + url.PathEscape(args[1]) + "/" + plural
			if exact {
				path += "/" + url.PathEscape(args[2])
			} else {
				after := int64(0)
				var err error
				if cursor != "" {
					after, err = strconv.ParseInt(cursor, 10, 64)
				}
				if err != nil || after < 0 || limit < 1 || limit > 100 {
					return usageError{errors.New("cursor must be non-negative and limit between 1 and 100")}
				}
				path += "?" + url.Values{"cursor": {cursor}, "limit": {strconv.Itoa(limit)}}.Encode()
			}
			var response json.RawMessage
			if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}}
		if !exact {
			cmd.Flags().StringVar(&cursor, "cursor", "", "Continue after this record version")
			cmd.Flags().IntVar(&limit, "limit", 20, "Maximum records (1–100)")
		}
		commands = append(commands, cmd)
	}
	return commands
}
