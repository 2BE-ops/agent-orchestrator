package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Routing attribution commands are read-only HTTP clients over sealed Manager
// decisions coupled with the derived fate of each routed task.
func addAgentManagerAttributionCommands(root *cobra.Command, ctx *commandContext) {
	var after int64
	var limit int
	outcomes := &cobra.Command{Use: "routing-outcomes <project>", Short: "Read routing decisions coupled with the fate of their routed tasks", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := agentManagerPath(args[0])
		if err != nil {
			return err
		}
		if after < 0 || limit < 1 || limit > 100 {
			return usageError{errors.New("after must be non-negative and limit must be 1 to 100")}
		}
		path += "/routing-outcomes?" + url.Values{"after": {strconv.FormatInt(after, 10)}, "limit": {strconv.Itoa(limit)}}.Encode()
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	outcomes.Flags().Int64Var(&after, "after", 0, "Continue after this request arrival sequence")
	outcomes.Flags().IntVar(&limit, "limit", 20, "Page size (1 to 100)")
	root.AddCommand(outcomes)

	var from, to string
	summary := &cobra.Command{Use: "routing-summary <project>", Short: "Aggregate one complete routing-decision cohort", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := agentManagerPath(args[0])
		if err != nil {
			return err
		}
		window, err := attributionWindowValues(from, to)
		if err != nil {
			return err
		}
		path += "/routing-summary?" + window.Encode()
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	summary.Flags().StringVar(&from, "from", "", "Window start as RFC3339 (required)")
	summary.Flags().StringVar(&to, "to", "", "Window end as RFC3339 (required)")
	root.AddCommand(summary)
}

// attributionWindowValues parses the shared from/to attribution window flags.
func attributionWindowValues(from, to string) (url.Values, error) {
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
		return nil, usageError{errors.New("from and to are required RFC3339 timestamps")}
	}
	start, err := time.Parse(time.RFC3339Nano, from)
	if err != nil {
		return nil, usageError{errors.New("from must be an RFC3339 timestamp")}
	}
	end, err := time.Parse(time.RFC3339Nano, to)
	if err != nil || !start.Before(end) {
		return nil, usageError{errors.New("to must be an RFC3339 timestamp after from")}
	}
	return url.Values{"from": {from}, "to": {to}}, nil
}
