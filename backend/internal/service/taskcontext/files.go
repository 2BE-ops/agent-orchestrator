package taskcontext

import (
	"io"
	"os"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Known credential paths are never context inputs. This is a path screen, not a
// claim that arbitrary source code has been scanned for embedded credentials.
func sensitivePath(name string) bool {
	for _, part := range strings.Split(strings.ToLower(name), "/") {
		if part == ".git" || part == ".ssh" || part == ".aws" || part == ".gnupg" || part == ".azure" {
			return true
		}
	}
	base := strings.ToLower(path.Base(name))
	if base == ".env" || strings.HasPrefix(base, ".env.") {
		return base != ".env.example" && base != ".env.sample" && base != ".env.template"
	}
	return base == "credentials" || base == "credentials.json" || base == "auth.json" || base == "id_rsa" || base == "id_ed25519" || base == ".npmrc" || base == ".netrc" || strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || strings.HasSuffix(base, ".p12") || strings.HasSuffix(base, ".pfx")
}

func workspaceSource(workspace, name string, maxBytes int) domain.ContextSource {
	source := domain.ContextSource{Kind: "file", ID: name, Disposition: "omitted", Reason: "Workspace file is unavailable or is not a regular confined file"}
	if !domain.ValidContextFilePath(name) || sensitivePath(name) {
		source.Reason = "Path is not eligible for workspace context"
		return source
	}
	root, err := os.OpenRoot(workspace)
	if err != nil {
		return source
	}
	defer func() { _ = root.Close() }()
	var expected os.FileInfo
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return source
		}
		if i < len(parts)-1 && !info.IsDir() {
			return source
		}
		expected = info
	}
	if !expected.Mode().IsRegular() {
		return source
	}
	if expected.Size() > int64(maxBytes) {
		source.Reason = "Workspace file exceeds the source byte budget"
		return source
	}
	file, err := root.Open(name)
	if err != nil {
		return source
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(expected, opened) {
		return source
	}
	content, err := io.ReadAll(io.LimitReader(file, int64(maxBytes)+1))
	if err != nil {
		return source
	}
	after, err := file.Stat()
	if err != nil || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) {
		source.Reason = "Workspace file changed while context was being read"
		return source
	}
	if len(content) > maxBytes || len(content) == 0 || !utf8.Valid(content) || strings.ContainsRune(string(content), 0) {
		source.Reason = "Workspace file is empty, binary, non-UTF-8 or exceeds the source byte budget"
		return source
	}
	source.Content, source.ContentHash = string(content), domain.ContextTextHash(string(content))
	source.SourceHash, source.Disposition = source.ContentHash, "inline"
	source.Reason = "Explicit task file selection from the provisioned worker workspace"
	return source
}
