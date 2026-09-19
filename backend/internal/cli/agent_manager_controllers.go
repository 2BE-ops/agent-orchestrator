package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

func addAgentManagerControllerCommands(root *cobra.Command, ctx *commandContext) {
	for _, spec := range []struct {
		use, suffix, description string
		args                     int
		start                    bool
	}{
		{"start <project>", "/controllers", "Start the configured native Manager using a stable admission ID", 1, true},
		{"current <project>", "/controller", "Inspect current reserved Manager ownership", 1, false},
		{"controller <project> <controller-id>", "/controllers", "Inspect an exact retained native admission", 2, false},
	} {
		var file string
		command := &cobra.Command{Use: spec.use, Short: spec.description, Args: usageArgs(cobra.ExactArgs(spec.args)), RunE: func(cmd *cobra.Command, args []string) error {
			path, err := agentManagerPath(args[0])
			if err != nil {
				return err
			}
			path += spec.suffix
			if spec.args == 2 {
				if strings.TrimSpace(args[1]) == "" || len(args[1]) > 200 || strings.IndexFunc(args[1], unicode.IsControl) >= 0 {
					return usageError{errors.New("controller identity must be 1 to 200 bytes without control characters")}
				}
				path += "/" + url.PathEscape(args[1])
			}
			var response json.RawMessage
			if spec.start {
				body, err := readAPIRequestJSON(cmd.InOrStdin(), file, 16<<10)
				if err != nil {
					return err
				}
				if err := ctx.postJSON(cmd.Context(), path, body, &response); err != nil {
					return err
				}
			} else if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}}
		if spec.start {
			command.Flags().StringVar(&file, "file", "", "Start request JSON file, or - for stdin (required)")
		}
		root.AddCommand(command)
	}
}
