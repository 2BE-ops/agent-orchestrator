package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Task commands author durable work through HTTP; they never acquire leases or
// bypass scheduler admission. JSON payloads mirror the public daemon contract.
func newTaskCommand(ctx *commandContext) *cobra.Command {
	root := &cobra.Command{Use: "task", Aliases: []string{"tasks"}, Short: "Author persistent tasks and inspect planning/attempt history (JSON output)"}
	for _, spec := range []struct {
		use, suffix, short     string
		project, page, version bool
	}{
		{"list <project>", "", "List a project's tasks", true, true, false},
		{"show <id>", "", "Inspect task planning and lease facts", false, false, false},
		{"revisions <id>", "/revisions", "List immutable task revisions", false, true, false},
		{"revision <id> <version>", "/revisions", "Inspect an exact planning revision", false, false, true},
		{"criteria <id> <version>", "/criteria", "Inspect exact acceptance criteria", false, false, true},
		{"audit <id>", "/audit", "Inspect durable task actions", false, true, false},
		{"attempts <id>", "/attempts", "Inspect frozen attempts and worker associations", false, true, false},
	} {
		var cursor string
		var limit int
		argCount := 1
		if spec.version {
			argCount = 2
		}
		cmd := &cobra.Command{Use: spec.use, Short: spec.short, Args: usageArgs(cobra.ExactArgs(argCount)), RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[0]) == "" {
				return usageError{errors.New("project or task id must not be blank")}
			}
			path := "tasks/" + url.PathEscape(args[0]) + spec.suffix
			if spec.project {
				path = "projects/" + url.PathEscape(args[0]) + "/tasks"
			}
			if spec.version {
				version, err := strconv.ParseInt(args[1], 10, 64)
				if err != nil || version < 1 {
					return usageError{errors.New("version must be positive")}
				}
				path += "/" + strconv.FormatInt(version, 10)
			}
			if spec.page {
				if limit < 1 || limit > 100 {
					return usageError{errors.New("limit must be between 1 and 100")}
				}
				if !spec.project && cursor != "" {
					after, err := strconv.ParseInt(cursor, 10, 64)
					if err != nil || after < 0 {
						return usageError{errors.New("history cursor must be a non-negative integer")}
					}
				}
				query := url.Values{"cursor": {cursor}, "limit": {fmt.Sprint(limit)}}
				path += "?" + query.Encode()
			}
			var response json.RawMessage
			if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}}
		if spec.page {
			cmd.Flags().StringVar(&cursor, "cursor", "", "Continue from the previous nextCursor")
			cmd.Flags().IntVar(&limit, "limit", 20, "Maximum entries (1–100)")
		}
		root.AddCommand(cmd)
	}
	for _, spec := range []struct {
		use, suffix, short string
		project            bool
	}{
		{"create <project>", "", "Create task intent and optional acceptance criteria", true},
		{"revise <id>", "/revisions", "Append task planning with expectedRevision and reason", false},
		{"set-criteria <id>", "/criteria", "Version future acceptance criteria with expectedRevision and reason", false},
	} {
		var file string
		cmd := &cobra.Command{Use: spec.use, Short: spec.short, Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[0]) == "" {
				return usageError{errors.New("project or task id must not be blank")}
			}
			body, err := readAPIRequestJSON(cmd.InOrStdin(), file, 256<<10)
			if err != nil {
				return err
			}
			path := "tasks/" + url.PathEscape(args[0]) + spec.suffix
			if spec.project {
				path = "projects/" + url.PathEscape(args[0]) + "/tasks"
			}
			var response json.RawMessage
			if err := ctx.doJSON(cmd.Context(), http.MethodPost, path, body, &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}}
		cmd.Flags().StringVar(&file, "file", "", "API request JSON file, or - for stdin (required)")
		root.AddCommand(cmd)
	}
	return root
}
