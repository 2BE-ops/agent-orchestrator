package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

func addAgentManagerProposalCommands(root *cobra.Command, ctx *commandContext) {
	for _, spec := range []struct {
		use, description string
		args             int
		native           bool
	}{
		{"propose <session-id> <request>", "Submit native output with its source generation and retry key", 2, true},
		{"proposals <project> <request>", "Inspect at most five retained native proposals", 2, false},
		{"proposal <project> <request> <proposal>", "Inspect exact native output and parser outcome", 3, false},
	} {
		var file string
		command := &cobra.Command{Use: spec.use, Short: spec.description, Args: usageArgs(cobra.ExactArgs(spec.args)), RunE: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if strings.TrimSpace(arg) == "" || len(arg) > 200 || strings.IndexFunc(arg, unicode.IsControl) >= 0 {
					return usageError{errors.New("identities must be nonempty, at most 200 bytes and contain no control characters")}
				}
			}
			path := "sessions/" + url.PathEscape(args[0]) + "/agent-manager"
			if !spec.native {
				var err error
				path, err = agentManagerPath(args[0])
				if err != nil {
					return err
				}
			}
			path += "/requests/" + url.PathEscape(args[1]) + "/proposals"
			if spec.args == 3 {
				path += "/" + url.PathEscape(args[2])
			}
			var response json.RawMessage
			if spec.native {
				body, err := readAPIRequestJSON(cmd.InOrStdin(), file, 512<<10)
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
		if spec.native {
			command.Flags().StringVar(&file, "file", "", "Proposal envelope JSON file, or - for stdin (required)")
		}
		root.AddCommand(command)
	}
}
