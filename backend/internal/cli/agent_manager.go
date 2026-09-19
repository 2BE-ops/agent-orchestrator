package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
)

func newAgentManagerCommand(ctx *commandContext) *cobra.Command {
	root := &cobra.Command{Use: "agent-manager", Short: "Configure and inspect persistent project Manager governance (JSON output)"}
	for _, spec := range []struct {
		name, suffix, description string
		args                      int
		paged                     bool
	}{
		{"show <project>", "", "Inspect current desired configuration", 1, false},
		{"configurations <project>", "/configurations", "List immutable governance versions", 1, true},
		{"configuration <project> <version>", "/configurations", "Inspect an exact retained configuration", 2, false},
		{"audit <project>", "/audit", "Inspect retained governance and work provenance", 1, true},
		{"inbox <project>", "/inbox", "Inspect pending routing requests", 1, true},
		{"requests <project>", "/requests", "Inspect retained routing history", 1, true},
	} {
		var cursor int64
		var limit int
		command := &cobra.Command{Use: spec.name, Short: spec.description, Args: usageArgs(cobra.ExactArgs(spec.args)), RunE: func(cmd *cobra.Command, args []string) error {
			path, err := agentManagerPath(args[0])
			if err != nil {
				return err
			}
			path += spec.suffix
			if spec.args == 2 {
				number, err := strconv.ParseInt(args[1], 10, 64)
				if err != nil || number < 1 || number > 1000 {
					return usageError{errors.New("version must be 1 to 1000")}
				}
				path += "/" + strconv.FormatInt(number, 10)
			}
			if spec.paged {
				if cursor < 0 || limit < 1 || limit > 100 {
					return usageError{errors.New("cursor must be non-negative and limit must be 1 to 100")}
				}
				path += "?" + url.Values{"cursor": {strconv.FormatInt(cursor, 10)}, "limit": {strconv.Itoa(limit)}}.Encode()
			}
			var response json.RawMessage
			if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}}
		if spec.paged {
			command.Flags().Int64Var(&cursor, "cursor", 0, "Next cursor from the previous page")
			command.Flags().IntVar(&limit, "limit", 20, "Page size (1 to 100)")
		}
		root.AddCommand(command)
	}
	var file string
	configure := &cobra.Command{Use: "configure <project>", Short: "Version user governance from API JSON; does not launch a controller", Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := agentManagerPath(args[0])
		if err != nil {
			return err
		}
		body, err := readAPIRequestJSON(cmd.InOrStdin(), file, 64<<10)
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.doJSON(cmd.Context(), http.MethodPut, path, body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}}
	configure.Flags().StringVar(&file, "file", "", "API request JSON file, or - for stdin (required)")
	root.AddCommand(configure)
	addAgentManagerInboxCommands(root, ctx)
	addAgentManagerProposalCommands(root, ctx)
	return root
}

func agentManagerPath(project string) (string, error) {
	if strings.TrimSpace(project) == "" || len(project) > 200 || strings.IndexFunc(project, unicode.IsControl) >= 0 {
		return "", usageError{errors.New("project must be a non-empty ID up to 200 bytes without control characters")}
	}
	return "projects/" + url.PathEscape(project) + "/agent-manager", nil
}
