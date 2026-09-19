package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

func addAgentManagerCandidateCommands(root *cobra.Command, ctx *commandContext) {
	for _, exact := range []bool{false, true} {
		var cursor string
		var limit int
		var version int64
		use, argc := "candidates <project> <request>", 2
		if exact {
			use, argc = "candidate <project> <request> <type>", 3
		}
		command := &cobra.Command{Use: use, Short: "Check Manager permissions, task compatibility and native availability", Args: usageArgs(cobra.ExactArgs(argc)), RunE: func(cmd *cobra.Command, args []string) error {
			for _, value := range append(append([]string{}, args...), cursor) {
				if len(value) > 200 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
					return usageError{errors.New("identities and cursor must be at most 200 bytes without control characters")}
				}
			}
			for _, value := range args {
				if strings.TrimSpace(value) == "" {
					return usageError{errors.New("identities must be nonempty")}
				}
			}
			path, err := agentManagerPath(args[0])
			if err != nil {
				return err
			}
			path += "/requests/" + url.PathEscape(args[1]) + "/candidates"
			query := url.Values{}
			if exact {
				if version < 1 {
					return usageError{errors.New("--version must name an exact positive Type version")}
				}
				path += "/" + url.PathEscape(args[2])
				query.Set("version", strconv.FormatInt(version, 10))
			} else {
				if limit < 1 || limit > 20 || (cursor != "" && strings.TrimSpace(cursor) == "") {
					return usageError{errors.New("limit must be 1 to 20 and cursor must be a nonempty ID when supplied")}
				}
				query.Set("limit", strconv.Itoa(limit))
				if cursor != "" {
					query.Set("cursor", cursor)
				}
			}
			var response json.RawMessage
			if err := ctx.getJSON(cmd.Context(), path+"?"+query.Encode(), &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}}
		if exact {
			command.Flags().Int64Var(&version, "version", 0, "Exact Type version (required)")
		} else {
			command.Flags().IntVar(&limit, "limit", 20, "Maximum native candidate checks (1 to 20)")
			command.Flags().StringVar(&cursor, "cursor", "", "nextCursor from the previous page")
		}
		root.AddCommand(command)
	}
}
