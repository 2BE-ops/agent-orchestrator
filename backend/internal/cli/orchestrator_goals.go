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

// Orchestrator goal commands are thin HTTP clients: goal authoring is a human
// action, while plan/complete carry native output for the project's live
// orchestrator session. None of them acquire leases or spawn workers.
func addOrchestratorGoalCommands(root *cobra.Command, ctx *commandContext) {
	for _, spec := range []struct {
		use, description, cursor string
		args                     int
		file, page               bool
	}{
		{"goal <project>", "Read the current goal and the live orchestrator generation", "", 1, false, false},
		{"set-goal <project>", "Record a user-authored goal version from JSON", "", 1, true, false},
		{"goal-versions <project>", "List immutable goal history", "after", 1, false, true},
		{"completions <project>", "List retained verified goal completions", "afterId", 1, false, true},
		{"plan <project>", "Submit one native planning action from JSON", "", 1, true, false},
		{"complete <project>", "Submit a native goal-completion assessment from JSON", "", 1, true, false},
		{"feedback <project>", "Read derived per-task loop facts", "after", 1, false, true},
		{"receipts <project>", "List sealed native planning receipts", "afterId", 1, false, true},
		{"receipt <project> <receipt>", "Inspect one sealed planning receipt", "", 2, false, false},
	} {
		var file, afterID string
		var limit int
		command := &cobra.Command{Use: spec.use, Short: spec.description, Args: usageArgs(cobra.ExactArgs(spec.args)), RunE: func(cmd *cobra.Command, args []string) error {
			for _, arg := range append(append([]string{}, args...), afterID) {
				if len(arg) > 200 || strings.IndexFunc(arg, unicode.IsControl) >= 0 {
					return usageError{errors.New("identities must be at most 200 bytes and contain no control characters")}
				}
			}
			for _, arg := range args {
				if strings.TrimSpace(arg) == "" {
					return usageError{errors.New("identities must be nonempty")}
				}
			}
			path := "projects/" + url.PathEscape(args[0])
			switch {
			case strings.HasPrefix(spec.use, "set-goal"):
				path += "/goal"
			case strings.HasPrefix(spec.use, "goal-versions"):
				path += "/goal/versions"
			case strings.HasPrefix(spec.use, "completions"):
				path += "/goal/completions"
			case spec.args == 2:
				path += "/orchestrator/receipts/" + url.PathEscape(args[1])
			default:
				path += "/orchestrator/" + strings.Split(spec.use, " ")[0]
			}
			query := url.Values{}
			if spec.page {
				if limit < 1 || limit > 100 || strings.TrimSpace(afterID) == "" && afterID != "" {
					return usageError{errors.New("limit must be 1 to 100 and after must be a nonempty ID when supplied")}
				}
				query.Set("limit", strconv.Itoa(limit))
				if afterID != "" {
					query.Set(spec.cursor, afterID)
				}
			}
			var response json.RawMessage
			if spec.file {
				budget := int64(128 << 10)
				if strings.HasPrefix(spec.use, "complete") {
					budget = 32 << 10
				}
				if strings.HasPrefix(spec.use, "set-goal") {
					budget = 64 << 10
				}
				body, err := readAPIRequestJSON(cmd.InOrStdin(), file, budget)
				if err != nil {
					return err
				}
				if err := ctx.doJSON(cmd.Context(), http.MethodPost, path, body, &response); err != nil {
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
		if spec.file {
			command.Flags().StringVar(&file, "file", "", "API request JSON file, or - for stdin (required)")
		}
		if spec.page {
			command.Flags().IntVar(&limit, "limit", 20, "Maximum entries (1–100)")
			command.Flags().StringVar(&afterID, "after", "", "Continue after the last returned entry")
		}
		root.AddCommand(command)
	}
}
