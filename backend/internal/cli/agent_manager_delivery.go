package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

func addAgentManagerDeliveryCommands(root *cobra.Command, ctx *commandContext) {
	for _, spec := range []struct {
		use, suffix, description string
		args                     int
	}{
		{"contexts <project> <request>", "contexts", "Inspect bounded sealed native input history", 2},
		{"context <project> <request> <context>", "contexts", "Inspect exact classified input and native tool instructions", 3},
		{"deliveries <project> <request>", "deliveries", "Inspect native send history and uncertain outcomes", 2},
	} {
		root.AddCommand(&cobra.Command{Use: spec.use, Short: spec.description, Args: usageArgs(cobra.ExactArgs(spec.args)), RunE: func(cmd *cobra.Command, args []string) error {
			for _, arg := range args {
				if strings.TrimSpace(arg) == "" || len(arg) > 200 || strings.IndexFunc(arg, unicode.IsControl) >= 0 {
					return usageError{errors.New("identities must be nonempty, at most 200 bytes and contain no control characters")}
				}
			}
			path, err := agentManagerPath(args[0])
			if err != nil {
				return err
			}
			path += "/requests/" + url.PathEscape(args[1]) + "/" + spec.suffix
			if spec.args == 3 {
				path += "/" + url.PathEscape(args[2])
			}
			var response json.RawMessage
			if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}})
	}
}
