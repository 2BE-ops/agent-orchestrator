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

// Evolution commands are thin HTTP clients over sealed controlled experiments
// and recommendations. Every write body is authored JSON; evidence is always
// derived by the daemon, never posted.
func newEvolutionCommand(ctx *commandContext) *cobra.Command {
	root := &cobra.Command{Use: "evolution", Short: "Run controlled version experiments and inspect recommendations (JSON output)"}

	create := &cobra.Command{Use: "experiment-create <project>", Short: "Seal one pinned control/candidate comparison", Args: usageArgs(cobra.ExactArgs(1))}
	var createFile string
	create.Flags().StringVar(&createFile, "file", "", "Experiment JSON with kind, entryId, controlVersion, candidateVersion, hypothesis and minimumSamples (use - for stdin; required)")
	create.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := evolutionProjectPath(args[0])
		if err != nil {
			return err
		}
		body, err := readAPIRequestJSON(cmd.InOrStdin(), createFile, 64<<10)
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), path+"/experiments", body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	root.AddCommand(create)

	list := &cobra.Command{Use: "experiments <project>", Short: "Page sealed experiments", Args: usageArgs(cobra.ExactArgs(1))}
	var listAfter string
	var listLimit int
	list.Flags().StringVar(&listAfter, "after", "", "Continue after this experiment id")
	list.Flags().IntVar(&listLimit, "limit", 20, "Page size (1 to 100)")
	list.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := evolutionProjectPath(args[0])
		if err != nil {
			return err
		}
		if listLimit < 1 || listLimit > 100 || (listAfter != "" && strings.TrimSpace(listAfter) == "") {
			return usageError{errors.New("limit must be 1 to 100 and after must be a nonempty id when supplied")}
		}
		query := url.Values{"limit": {strconv.Itoa(listLimit)}}
		if listAfter != "" {
			query.Set("afterId", listAfter)
		}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path+"/experiments?"+query.Encode(), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	root.AddCommand(list)

	show := &cobra.Command{Use: "experiment <project> <experimentId>", Short: "Inspect one sealed experiment with any conclusion", Args: usageArgs(cobra.ExactArgs(2))}
	show.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := evolutionExperimentPath(args[0], args[1])
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	root.AddCommand(show)

	evidence := &cobra.Command{Use: "evidence <project> <experimentId>", Short: "Derive both comparable cohorts for one admission window", Args: usageArgs(cobra.ExactArgs(2))}
	var evidenceFrom, evidenceTo string
	evidence.Flags().StringVar(&evidenceFrom, "from", "", "Window start as RFC3339 (required)")
	evidence.Flags().StringVar(&evidenceTo, "to", "", "Window end as RFC3339 (required)")
	evidence.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := evolutionExperimentPath(args[0], args[1])
		if err != nil {
			return err
		}
		window, err := attributionWindowValues(evidenceFrom, evidenceTo)
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path+"/evidence?"+window.Encode(), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	root.AddCommand(evidence)

	conclude := &cobra.Command{Use: "conclude <project> <experimentId>", Short: "Seal the terminal decision with daemon-recomputed evidence", Args: usageArgs(cobra.ExactArgs(2))}
	var concludeFile string
	conclude.Flags().StringVar(&concludeFile, "file", "", "Conclusion JSON with outcome, reason, from and to (use - for stdin; required)")
	conclude.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := evolutionExperimentPath(args[0], args[1])
		if err != nil {
			return err
		}
		body, err := readAPIRequestJSON(cmd.InOrStdin(), concludeFile, 64<<10)
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), path+"/conclude", body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	root.AddCommand(conclude)

	experimentDiff := &cobra.Command{Use: "experiment-diff <project> <experimentId>", Short: "Read the immutable field diff between the pinned versions", Args: usageArgs(cobra.ExactArgs(2))}
	experimentDiff.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := evolutionExperimentPath(args[0], args[1])
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path+"/diff", &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	root.AddCommand(experimentDiff)

	recommend := &cobra.Command{Use: "recommend <project>", Short: "Persist one improvement proposal with a sealed proposed definition", Args: usageArgs(cobra.ExactArgs(1))}
	var recommendFile string
	recommend.Flags().StringVar(&recommendFile, "file", "", "Recommendation JSON with kind, entryId, fromVersion, observation, sampleSize and proposed (use - for stdin; required)")
	recommend.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := evolutionProjectPath(args[0])
		if err != nil {
			return err
		}
		body, err := readAPIRequestJSON(cmd.InOrStdin(), recommendFile, 288<<10)
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), path+"/recommendations", body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	root.AddCommand(recommend)

	recommendations := &cobra.Command{Use: "recommendations <project>", Short: "Page sealed recommendations", Args: usageArgs(cobra.ExactArgs(1))}
	var recommendationAfter string
	var recommendationLimit int
	recommendations.Flags().StringVar(&recommendationAfter, "after", "", "Continue after this recommendation id")
	recommendations.Flags().IntVar(&recommendationLimit, "limit", 20, "Page size (1 to 100)")
	recommendations.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := evolutionProjectPath(args[0])
		if err != nil {
			return err
		}
		if recommendationLimit < 1 || recommendationLimit > 100 || (recommendationAfter != "" && strings.TrimSpace(recommendationAfter) == "") {
			return usageError{errors.New("limit must be 1 to 100 and after must be a nonempty id when supplied")}
		}
		query := url.Values{"limit": {strconv.Itoa(recommendationLimit)}}
		if recommendationAfter != "" {
			query.Set("afterId", recommendationAfter)
		}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path+"/recommendations?"+query.Encode(), &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	root.AddCommand(recommendations)

	recommendation := &cobra.Command{Use: "recommendation <project> <recommendationId>", Short: "Inspect one sealed recommendation", Args: usageArgs(cobra.ExactArgs(2))}
	recommendation.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := evolutionRecommendationPath(args[0], args[1])
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	root.AddCommand(recommendation)

	decide := &cobra.Command{Use: "decide <project> <recommendationId>", Short: "Seal the one-time dismissal or adoption decision", Args: usageArgs(cobra.ExactArgs(2))}
	var decideFile string
	decide.Flags().StringVar(&decideFile, "file", "", "Decision JSON with disposition and reason (use - for stdin; required)")
	decide.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := evolutionRecommendationPath(args[0], args[1])
		if err != nil {
			return err
		}
		body, err := readAPIRequestJSON(cmd.InOrStdin(), decideFile, 64<<10)
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.postJSON(cmd.Context(), path+"/decide", body, &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	root.AddCommand(decide)

	recommendationDiff := &cobra.Command{Use: "recommendation-diff <project> <recommendationId>", Short: "Read the inspectable diff from the sealed version to the proposal", Args: usageArgs(cobra.ExactArgs(2))}
	recommendationDiff.RunE = func(cmd *cobra.Command, args []string) error {
		path, err := evolutionRecommendationPath(args[0], args[1])
		if err != nil {
			return err
		}
		var response json.RawMessage
		if err := ctx.getJSON(cmd.Context(), path+"/diff", &response); err != nil {
			return err
		}
		return writeJSON(cmd.OutOrStdout(), response)
	}
	root.AddCommand(recommendationDiff)

	return root
}

func evolutionProjectPath(project string) (string, error) {
	if strings.TrimSpace(project) == "" || len(project) > 200 || strings.IndexFunc(project, unicode.IsControl) >= 0 {
		return "", usageError{errors.New("project must be a non-empty ID up to 200 bytes without control characters")}
	}
	return "projects/" + url.PathEscape(project), nil
}

func evolutionExperimentPath(project, experimentID string) (string, error) {
	path, err := evolutionProjectPath(project)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(experimentID) == "" || len(experimentID) > 200 || strings.IndexFunc(experimentID, unicode.IsControl) >= 0 {
		return "", usageError{errors.New("experiment id must be a non-empty ID up to 200 bytes without control characters")}
	}
	return path + "/experiments/" + url.PathEscape(experimentID), nil
}

func evolutionRecommendationPath(project, recommendationID string) (string, error) {
	path, err := evolutionProjectPath(project)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(recommendationID) == "" || len(recommendationID) > 200 || strings.IndexFunc(recommendationID, unicode.IsControl) >= 0 {
		return "", usageError{errors.New("recommendation id must be a non-empty ID up to 200 bytes without control characters")}
	}
	return path + "/recommendations/" + url.PathEscape(recommendationID), nil
}
