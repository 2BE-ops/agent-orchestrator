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

func addAgentManagerRegistryCommands(root *cobra.Command, ctx *commandContext) {
	for _, spec := range []struct {
		use, description string
		args             int
		native           bool
	}{
		{"registry-author <session-id> <request>", "Submit governed native registry authoring output with its retry key", 2, true},
		{"registry-receipts <project> <request>", "Inspect a request's sealed registry authoring receipts", 2, false},
		{"registry-receipt <project> <request> <receipt>", "Inspect one sealed registry authoring receipt", 3, false},
	} {
		var file, after string
		var limit int
		command := &cobra.Command{Use: spec.use, Short: spec.description, Args: usageArgs(cobra.ExactArgs(spec.args)), RunE: func(cmd *cobra.Command, args []string) error {
			for _, arg := range append(append([]string{}, args...), after) {
				if len(arg) > 200 || strings.IndexFunc(arg, unicode.IsControl) >= 0 {
					return usageError{errors.New("identities must be at most 200 bytes and contain no control characters")}
				}
			}
			for _, arg := range args {
				if strings.TrimSpace(arg) == "" {
					return usageError{errors.New("identities must be nonempty")}
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
			path += "/requests/" + url.PathEscape(args[1]) + "/registry-receipts"
			if spec.args == 3 {
				path += "/" + url.PathEscape(args[2])
			}
			query := url.Values{}
			if spec.args == 2 && !spec.native {
				if limit < 1 || limit > 100 || (after != "" && strings.TrimSpace(after) == "") {
					return usageError{errors.New("limit must be 1 to 100 and after must be a nonempty ID when supplied")}
				}
				query.Set("limit", strconv.Itoa(limit))
				if after != "" {
					query.Set("afterId", after)
				}
			}
			var response json.RawMessage
			if spec.native {
				body, err := readAPIRequestJSON(cmd.InOrStdin(), file, 288<<10)
				if err != nil {
					return err
				}
				path = "sessions/" + url.PathEscape(args[0]) + "/agent-manager/requests/" + url.PathEscape(args[1]) + "/registry-actions"
				if err := ctx.postJSON(cmd.Context(), path, body, &response); err != nil {
					return err
				}
			} else {
				target := path
				if encoded := query.Encode(); encoded != "" {
					target += "?" + encoded
				}
				if err := ctx.getJSON(cmd.Context(), target, &response); err != nil {
					return err
				}
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}}
		if spec.native {
			command.Flags().StringVar(&file, "file", "", "Registry authoring envelope JSON file, or - for stdin (required)")
		} else if spec.args == 2 {
			command.Flags().IntVar(&limit, "limit", 20, "Maximum receipts per page (1 to 100)")
			command.Flags().StringVar(&after, "after", "", "nextAfterId from the previous page")
		}
		root.AddCommand(command)
	}
}
