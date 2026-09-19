package taskverify

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/process"
)

func artifactGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := process.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

func TestArtifactCollectorReadsRawCommitBlobsAndBoundsUnsupportedSources(t *testing.T) {
	repo := t.TempDir()
	artifactGit(t, repo, "init", "-q")
	for name, body := range map[string]string{"artifact[x].txt": "committed artifact", ".gitattributes": "artifact[x].txt export-ignore\n", "large.bin": strings.Repeat("x", (1<<20)+1)} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	artifactGit(t, repo, "add", ".")
	blob := artifactGit(t, repo, "hash-object", "artifact[x].txt")
	artifactGit(t, repo, "update-index", "--add", "--cacheinfo", "120000,"+blob+",link")
	artifactGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", "artifact")
	commit := artifactGit(t, repo, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(repo, "artifact[x].txt"), []byte("dirty worker claim"), 0600); err != nil {
		t.Fatal(err)
	}
	plan := domain.TaskEvaluationPreparation{WorkspacePath: repo, Result: domain.TaskResult{Definition: domain.TaskResultDefinition{ClaimedCommit: commit}}}
	for _, path := range []string{"artifact[x].txt", "missing", "link", "large.bin"} {
		plan.Criteria.Definition.Criteria = append(plan.Criteria.Definition.Criteria, domain.AcceptanceCriterion{ID: path, EvidenceKind: "artifact", ArtifactPath: path, ArtifactSHA256: domain.ContextTextHash("committed artifact")})
	}
	items, err := (Artifacts{}).Collect(context.Background(), plan)
	if err != nil || len(items) != 4 {
		t.Fatalf("collection: %+v %v", items, err)
	}
	for i, state := range []string{"observed", "missing", "unsupported", "oversized"} {
		if items[i].State != state || items[i].Validate() != nil {
			t.Fatalf("source %d: %+v", i, items[i])
		}
	}
	if items[0].SHA256 != domain.ContextTextHash("committed artifact") || items[0].GitBlobID != blob {
		t.Fatalf("working tree or attributes replaced Git blob: %+v", items[0])
	}
	plan.Result.Definition.ClaimedCommit = strings.Repeat("f", 40)
	items, err = (Artifacts{}).Collect(context.Background(), plan)
	if err != nil || items[0].State != "unavailable" {
		t.Fatalf("missing commit became missing artifact: %+v %v", items, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Artifacts{}).Collect(ctx, plan); err == nil {
		t.Fatal("cancelled collection accepted")
	}
}

func TestArtifactCollectorCapsSelectionAndGitOutput(t *testing.T) {
	plan := domain.TaskEvaluationPreparation{Result: domain.TaskResult{Definition: domain.TaskResultDefinition{ClaimedCommit: strings.Repeat("a", 40)}}}
	for range 64 {
		plan.Criteria.Definition.Criteria = append(plan.Criteria.Definition.Criteria, domain.AcceptanceCriterion{ID: "artifact", EvidenceKind: "artifact", ArtifactPath: "file", ArtifactSHA256: strings.Repeat("b", 64)})
	}
	items, err := (Artifacts{}).Collect(context.Background(), plan)
	if err != nil || len(items) != 16 {
		t.Fatalf("unbounded selection: %d %v", len(items), err)
	}
	cancelled := false
	w := &boundedGitOutput{limit: 4, cancel: func() { cancelled = true }}
	if _, err := w.Write([]byte("12345")); err == nil || !cancelled || w.Len() != 0 {
		t.Fatalf("output bound failed: %d %v", w.Len(), err)
	}
	cancelled = false
	w = &boundedGitOutput{limit: 4, cancel: func() { cancelled = true }}
	// io.Copy may use ReaderFrom instead of Write. Embedding bytes.Buffer would
	// expose its unbounded fast path and bypass the limit on subprocess output.
	if _, err := io.Copy(w, io.LimitReader(strings.NewReader("12345"), 5)); err == nil || !cancelled || w.Len() != 0 {
		t.Fatalf("copy bypassed bound: %d %v", w.Len(), err)
	}
}
