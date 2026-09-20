package taskcontext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkspaceContextReadsOnlyBoundedExplicitRegularText(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct{ name, content, disposition string }{
		{"source.txt", "exact\r\nUTF-8 ✓", "inline"},
		{".env", "SECRET=never-inline", "omitted"},
		{".env.production", "SECRET=never-inline", "omitted"},
		{".env.example", "KEY=placeholder", "inline"},
		{"auth.json", "never-inline", "omitted"},
		{"id_ed25519", "never-inline", "omitted"},
		{".npmrc", "never-inline", "omitted"},
		{"private.pem", "never-inline", "omitted"},
		{"binary.txt", "zero\x00byte", "omitted"},
		{"invalid.txt", "invalid\xffUTF8", "omitted"},
		{"empty.txt", "", "omitted"},
		{"large.txt", strings.Repeat("x", 257), "omitted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, tc.name), []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			source := workspaceSource(root, tc.name, 256)
			if source.Disposition != tc.disposition || (tc.disposition == "omitted" && source.Content != "") || (tc.disposition == "inline" && source.Content != tc.content) {
				t.Fatalf("unexpected file selection: %+v", source)
			}
		})
	}
	for _, name := range []string{"missing.txt", "../outside.txt", "/absolute", ".git/config"} {
		if source := workspaceSource(root, name, 256); source.Disposition != "omitted" || source.Content != "" || strings.Contains(source.Reason, root) {
			t.Fatalf("unsafe/unavailable input: %+v", source)
		}
	}
}

func TestWorkspaceContextRejectsSymlinksEvenInsideRoot(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "safe.txt"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "outside.txt")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := os.Symlink(filepath.Join(root, "safe.txt"), filepath.Join(root, "inside.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked-dir")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"outside.txt", "inside.txt", "linked-dir/secret.txt"} {
		if source := workspaceSource(root, name, 256); source.Disposition != "omitted" || source.Content != "" {
			t.Fatalf("followed symlink: %+v", source)
		}
	}
}
