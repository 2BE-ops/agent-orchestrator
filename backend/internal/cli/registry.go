package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Registry commands remain HTTP clients. File bodies use the documented API
// contract; no command accesses SQLite or launches an agent adapter directly.
func newRegistryCommand(ctx *commandContext, name, resource string) *cobra.Command {
	root := &cobra.Command{Use: name, Aliases: []string{resource}, Short: "Manage versioned " + resource + " (JSON output)"}
	for _, spec := range []struct {
		use, suffix, short string
		args               int
	}{
		{"list", "", "List definitions", 0},
		{"show <id>", "", "Inspect the active definition", 1},
		{"versions <id>", "/versions", "List immutable versions", 1},
		{"audit <id>", "/audit", "Inspect authoring history", 1},
	} {
		var cursor string
		var limit int
		cmd := &cobra.Command{Use: spec.use, Short: spec.short, Args: usageArgs(cobra.ExactArgs(spec.args)),
			RunE: func(cmd *cobra.Command, args []string) error {
				if limit < 1 || limit > 200 {
					return usageError{errors.New("limit must be between 1 and 200")}
				}
				path := resource
				if len(args) != 0 {
					if strings.TrimSpace(args[0]) == "" {
						return usageError{errors.New("definition id must not be blank")}
					}
					path += "/" + url.PathEscape(args[0]) + spec.suffix
				}
				query := url.Values{"limit": {fmt.Sprint(limit)}, "cursor": {cursor}}
				var response json.RawMessage
				if err := ctx.getJSON(cmd.Context(), path+"?"+query.Encode(), &response); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), response)
			}}
		cmd.Flags().StringVar(&cursor, "cursor", "", "Continue from the previous nextCursor")
		cmd.Flags().IntVar(&limit, "limit", 100, "Maximum entries (1–200)")
		root.AddCommand(cmd)
	}
	for _, spec := range []struct {
		use, suffix, method, short string
		args                       int
	}{
		{"create", "", http.MethodPost, "Create from an API JSON object", 0},
		{"new-version <id>", "/versions", http.MethodPost, "Append an inactive configuration version", 1},
		{"update <id>", "", http.MethodPatch, "Update metadata/policy with an expected revision", 1},
		{"activate <id>", "/activate", http.MethodPost, "Activate or roll back using an expected revision", 1},
		{"clone <id>", "/clone", http.MethodPost, "Clone an exact source version", 1},
	} {
		var file string
		cmd := &cobra.Command{Use: spec.use, Short: spec.short, Args: usageArgs(cobra.ExactArgs(spec.args)),
			RunE: func(cmd *cobra.Command, args []string) error {
				path := resource
				if len(args) != 0 {
					if strings.TrimSpace(args[0]) == "" {
						return usageError{errors.New("definition id must not be blank")}
					}
					path += "/" + url.PathEscape(args[0]) + spec.suffix
				}
				body, err := readRegistryJSON(cmd.InOrStdin(), file)
				if err != nil {
					return err
				}
				var response json.RawMessage
				if err := ctx.doJSON(cmd.Context(), spec.method, path, body, &response); err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), response)
			}}
		cmd.Flags().StringVar(&file, "file", "", "API request JSON file, or - for stdin (required)")
		root.AddCommand(cmd)
	}
	return root
}

func readRegistryJSON(stdin io.Reader, file string) (json.RawMessage, error) {
	if file == "" {
		return nil, usageError{errors.New("--file is required; use - for stdin")}
	}
	reader := stdin
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return nil, fmt.Errorf("read registry request: %w", err)
		}
		defer func() { _ = f.Close() }()
		reader = f
	}
	content, err := io.ReadAll(io.LimitReader(reader, (2<<20)+1))
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(content))
	if len(content) > 2<<20 || !json.Valid(content) || !strings.HasPrefix(trimmed, "{") {
		return nil, usageError{errors.New("request must be one JSON object no larger than 2 MiB")}
	}
	return json.RawMessage(content), nil
}
