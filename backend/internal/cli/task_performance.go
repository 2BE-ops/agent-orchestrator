package cli

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/spf13/cobra"
)

func newTaskPerformanceCommands(ctx *commandContext) []*cobra.Command {
	commands := make([]*cobra.Command, 0, 2)
	for _, summary := range []bool{false, true} {
		var from, to, cursor, group string
		var limit int
		name, description := "performance", "Inspect attempt cohort evidence and session-wide native usage"
		if summary {
			name, description = "metrics", "Summarize a complete attempt cohort with sample counts and exclusions"
		}
		command := &cobra.Command{Use: name + " <project>", Short: description, Args: usageArgs(cobra.ExactArgs(1)), RunE: func(cmd *cobra.Command, args []string) error {
			start, startErr := time.Parse(time.RFC3339Nano, from)
			end, endErr := time.Parse(time.RFC3339Nano, to)
			if strings.TrimSpace(args[0]) == "" || len(args[0]) > 200 || strings.IndexFunc(args[0], unicode.IsControl) >= 0 || startErr != nil || endErr != nil || start.IsZero() || end.IsZero() || !start.Before(end) || end.Sub(start) > 366*24*time.Hour {
				return usageError{errors.New("project, --from and --to are required; use an RFC3339 admission window up to 366 days")}
			}
			query := url.Values{"from": {from}, "to": {to}}
			path := "projects/" + url.PathEscape(args[0]) + "/task-performance"
			if summary {
				switch group {
				case "agent_type", "agent_type_version", "skill", "skill_version", "harness", "model", "category", "capability":
				default:
					return usageError{errors.New("unsupported --group-by dimension")}
				}
				path += "/summary"
				query.Set("groupBy", group)
			} else {
				if limit < 1 || limit > 100 || len(cursor) > 200 || strings.IndexFunc(cursor, unicode.IsControl) >= 0 {
					return usageError{errors.New("limit must be 1 to 100 and cursor must be a bounded attempt ID")}
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
		command.Flags().StringVar(&from, "from", "", "Inclusive attempt admission timestamp (RFC3339, required)")
		command.Flags().StringVar(&to, "to", "", "Exclusive attempt admission timestamp (RFC3339, required)")
		if summary {
			command.Flags().StringVar(&group, "group-by", "agent_type_version", "agent_type, agent_type_version, skill, skill_version, harness, model, category or capability")
		} else {
			command.Flags().IntVar(&limit, "limit", 20, "Page size (1 to 100)")
			command.Flags().StringVar(&cursor, "cursor", "", "Next cursor from a previous evidence page")
		}
		commands = append(commands, command)
	}
	return commands
}
