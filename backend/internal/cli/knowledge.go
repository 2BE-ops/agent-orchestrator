package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func newKnowledgeCommand(ctx *commandContext) *cobra.Command {
	root := &cobra.Command{Use: "knowledge", Short: "Author and inspect versioned project knowledge (JSON output)"}
	for _, spec := range []struct {
		use, suffix                      string
		project, version, history, write bool
	}{
		{"list <project>", "", true, false, false, false},
		{"show <id>", "", false, false, false, false},
		{"versions <id>", "/versions", false, false, true, false},
		{"version <id> <version>", "/versions", false, true, false, false},
		{"create <project>", "", true, false, false, true},
		{"revise <id>", "/versions", false, false, false, true},
	} {
		var file, cursor, status, kind, search string
		var limit int
		argCount := 1
		if spec.version {
			argCount = 2
		}
		cmd := &cobra.Command{Use: spec.use, Short: "Use the daemon knowledge API", Args: usageArgs(cobra.ExactArgs(argCount)), RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(args[0]) == "" {
				return usageError{errors.New("project or knowledge id must not be blank")}
			}
			path := "knowledge/" + url.PathEscape(args[0]) + spec.suffix
			if spec.project {
				path = "projects/" + url.PathEscape(args[0]) + "/knowledge"
			}
			if spec.version {
				number, err := strconv.ParseInt(args[1], 10, 64)
				if err != nil || number < 1 {
					return usageError{errors.New("version must be positive")}
				}
				path += "/" + strconv.FormatInt(number, 10)
			}
			method := http.MethodGet
			var body any
			if spec.write {
				var err error
				body, err = readAPIRequestJSON(cmd.InOrStdin(), file, 128<<10)
				if err != nil {
					return err
				}
				method = http.MethodPost
			} else if spec.project || spec.history {
				if limit < 1 || limit > 100 {
					return usageError{errors.New("limit must be between 1 and 100")}
				}
				if spec.history && cursor != "" {
					after, err := strconv.ParseInt(cursor, 10, 64)
					if err != nil || after < 0 {
						return usageError{errors.New("history cursor must be non-negative")}
					}
				}
				query := url.Values{"limit": {strconv.Itoa(limit)}, "cursor": {cursor}}
				if spec.project {
					query.Set("status", status)
					query.Set("kind", kind)
					query.Set("search", search)
				}
				path += "?" + query.Encode()
			}
			var response json.RawMessage
			if err := ctx.doJSON(cmd.Context(), method, path, body, &response); err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), response)
		}}
		if spec.write {
			cmd.Flags().StringVar(&file, "file", "", "API request JSON file, or - for stdin (required)")
		} else if spec.project || spec.history {
			cmd.Flags().StringVar(&cursor, "cursor", "", "Continue from the previous nextCursor")
			cmd.Flags().IntVar(&limit, "limit", 20, "Maximum entries (1–100)")
			if spec.project {
				cmd.Flags().StringVar(&status, "status", "", "Filter by review status; default excludes deleted")
				cmd.Flags().StringVar(&kind, "kind", "", "Filter by knowledge kind")
				cmd.Flags().StringVar(&search, "search", "", "Literal text in current title or content")
			}
		}
		root.AddCommand(cmd)
	}
	return root
}
