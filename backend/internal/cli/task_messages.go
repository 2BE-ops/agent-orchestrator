package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newTaskMessageCommands(ctx *commandContext) []*cobra.Command {
	var file, taskID, cursor string
	var limit int
	send := &cobra.Command{Use: "send-message <session-id>", Short: "Persist bounded typed worker coordination", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(args[0]) == "" {
			return usageError{errors.New("worker session id must not be blank")}
		}
		body, err := readAPIRequestJSON(cmd.InOrStdin(), file, 64<<10)
		if err != nil {
			return err
		}
		var receipt json.RawMessage
		if err := ctx.postJSON(cmd.Context(), "sessions/"+url.PathEscape(args[0])+"/task-messages", body, &receipt); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), receipt)
	}}
	send.Flags().StringVar(&file, "file", "", "Message request JSON file, or - for stdin (required)")
	list := &cobra.Command{Use: "messages <project>", Short: "Inspect the shared project message timeline", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(args[0]) == "" || (cmd.Flags().Changed("task") && strings.TrimSpace(taskID) == "") {
			return usageError{errors.New("project and selected task ids must not be blank")}
		}
		after := int64(0)
		var err error
		if cursor != "" {
			after, err = strconv.ParseInt(cursor, 10, 64)
		}
		if err != nil || after < 0 || limit < 1 || limit > 100 {
			return usageError{errors.New("cursor must be non-negative and limit between 1 and 100")}
		}
		query := url.Values{"cursor": {cursor}, "limit": {strconv.Itoa(limit)}, "taskId": {taskID}}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), "projects/"+url.PathEscape(args[0])+"/task-messages?"+query.Encode(), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	list.Flags().StringVar(&taskID, "task", "", "Filter messages sent or received by this task")
	list.Flags().StringVar(&cursor, "cursor", "", "Continue after this message sequence")
	list.Flags().IntVar(&limit, "limit", 20, "Maximum messages (1-100)")
	show := &cobra.Command{Use: "message <project> <message-id>", Short: "Inspect coordination content and delivery observations", Args: usageArgs(cobra.ExactArgs(2)), RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(args[0]) == "" || strings.TrimSpace(args[1]) == "" {
			return usageError{errors.New("project and message ids must not be blank")}
		}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), "projects/"+url.PathEscape(args[0])+"/task-messages/"+url.PathEscape(args[1]), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	return []*cobra.Command{send, list, show}
}
