// Package taskverify collects local evidence without trusting worker claims.
package taskverify

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
	"github.com/aoagents/agent-orchestrator/backend/internal/process"
)

// Artifacts reads raw Git blobs, without checkout filters, replacement objects,
// hooks, network fetches, working-tree writes or execution of artifact content.
type Artifacts struct{}

var _ ports.TaskArtifactCollector = Artifacts{}

// Collect has a 15-second total deadline, at most 16 selected files, and a
// 1 MiB per-file bound. Missing local objects remain unavailable, never fetched.
func (Artifacts) Collect(parent context.Context, plan domain.TaskEvaluationPreparation) ([]domain.TaskArtifactEvidence, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	items := []domain.TaskArtifactEvidence{}
	target := plan.Result.Definition.ClaimedCommit
	if !fullObjectID(target) {
		return items, nil
	}
	for _, criterion := range plan.Criteria.Definition.Criteria {
		if criterion.EvidenceKind != "artifact" || criterion.ArtifactSHA256 == "" {
			continue
		}
		if len(items) == 16 {
			break
		}
		item := domain.TaskArtifactEvidence{Collector: "git-blob/v1", CriterionID: criterion.ID, TargetCommit: target, Path: criterion.ArtifactPath, State: "unavailable", Reason: "repository_unavailable", ObservedAt: time.Now().UTC()}
		if filepath.IsAbs(plan.WorkspacePath) && domain.ValidContextFilePath(criterion.ArtifactPath) {
			item = readArtifact(ctx, plan.WorkspacePath, item)
		}
		items = append(items, item)
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func readArtifact(ctx context.Context, repo string, item domain.TaskArtifactEvidence) domain.TaskArtifactEvidence {
	objectType, err := gitOutput(ctx, repo, 128, "cat-file", "-t", item.TargetCommit)
	if err != nil || strings.TrimSpace(string(objectType)) != "commit" {
		item.Reason = "commit_unavailable"
		return item
	}
	entry, err := gitOutput(ctx, repo, 4096, "ls-tree", "-z", item.TargetCommit, "--", item.Path)
	if err != nil {
		item.Reason = "tree_unavailable"
		return item
	}
	if len(entry) == 0 {
		item.State, item.Reason = "missing", "path_absent_at_commit"
		return item
	}
	metadata, path, found := strings.Cut(string(entry), "\t")
	fields := strings.Fields(metadata)
	if !found || path != item.Path+"\x00" || len(fields) != 3 || !fullObjectID(fields[2]) {
		item.Reason = "ambiguous_tree_entry"
		return item
	}
	if (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" {
		item.State, item.Reason = "unsupported", "regular_blob_required"
		return item
	}
	item.GitBlobID = fields[2]
	sizeOutput, err := gitOutput(ctx, repo, 128, "cat-file", "-s", item.GitBlobID)
	if err != nil {
		item.Reason = "blob_unavailable"
		return item
	}
	size, err := strconv.ParseInt(strings.TrimSpace(string(sizeOutput)), 10, 64)
	if err != nil || size < 0 {
		item.Reason = "invalid_blob_size"
		return item
	}
	item.Bytes = size
	if size > 1<<20 {
		item.State, item.Reason = "oversized", "artifact_exceeds_1_mib"
		return item
	}
	content, err := gitOutput(ctx, repo, 1<<20, "cat-file", "blob", item.GitBlobID)
	if err != nil || int64(len(content)) != size {
		item.Reason = "blob_read_incomplete"
		return item
	}
	item.SHA256 = domain.ContextTextHash(string(content))
	item.State, item.Reason = "observed", "raw_git_blob_read"
	return item
}

func fullObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

type boundedGitOutput struct {
	buffer bytes.Buffer
	limit  int
	cancel context.CancelFunc
}

func (w *boundedGitOutput) Len() int      { return w.buffer.Len() }
func (w *boundedGitOutput) Bytes() []byte { return w.buffer.Bytes() }

func (w *boundedGitOutput) Write(data []byte) (int, error) {
	if len(data) > w.limit-w.Len() {
		w.cancel()
		return 0, fmt.Errorf("git output exceeds verification bound")
	}
	return w.buffer.Write(data)
}

func gitOutput(parent context.Context, repo string, limit int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	argv := make([]string, 0, 7+len(args))
	argv = append(argv, "--no-pager", "--no-replace-objects", "--literal-pathspecs", "-c", "core.fsmonitor=false", "-C", repo)
	cmd := process.CommandContext(ctx, "git", append(argv, args...)...)
	for _, variable := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(variable), "GIT_") {
			cmd.Env = append(cmd.Env, variable)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0")
	output := &boundedGitOutput{limit: limit, cancel: cancel}
	cmd.Stdout, cmd.Stderr, cmd.Stdin = output, io.Discard, strings.NewReader("")
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
