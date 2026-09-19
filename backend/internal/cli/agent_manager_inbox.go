package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

func addAgentManagerInboxCommands(root *cobra.Command, ctx *commandContext) {
	for _, spec := range []struct {
		name, description, suffix string
		args                      int
		write                     bool
	}{
		{"request <project> <request>", "Inspect sealed routing intent", "", 2, false},
		{"request-resolution <project> <request>", "Inspect a terminal receipt or pending state", "/resolution", 2, false},
		{"enqueue <project>", "Queue exact routing intent from API JSON; does not launch sessions", "", 1, true},
		{"resolve <project> <request>", "Close routing intent from API JSON; does not stop workers", "/resolution", 2, true},
	} {
		var file string
		command := &cobra.Command{Use: spec.name, Short: spec.description, Args: usageArgs(cobra.ExactArgs(spec.args)), RunE: func(cmd *cobra.Command, args []string) error {
			path, err := agentManagerPath(args[0])
			if err != nil {
				return err
			}
			path += "/requests"
			if spec.args == 2 {
				if strings.TrimSpace(args[1]) == "" || len(args[1]) > 200 || strings.IndexFunc(args[1], unicode.IsControl) >= 0 {
					return usageError{errors.New("request must be a non-empty ID up to 200 bytes without control characters")}
				}
				path += "/" + url.PathEscape(args[1])
			}
			path += spec.suffix
			var response json.RawMessage
			if spec.write {
				body, err := readAPIRequestJSON(cmd.InOrStdin(), file, 16<<10)
				if err != nil {
					return err
				}
				if err := ctx.doJSON(cmd.Context(), http.MethodPost, path, body, &response); err != nil {
					return err
				}
			} else if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}}
		if spec.write {
			command.Flags().StringVar(&file, "file", "", "API request JSON file, or - for stdin (required)")
		}
		root.AddCommand(command)
	}
}
