package sessionmanager

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// workerSnapshotPrompt materializes only sealed inert resources beneath the AO
// data root. Native user skill folders and repository files are never modified.
func (m *Manager) workerSnapshotPrompt(ctx context.Context, id domain.SessionID, snapshot domain.WorkerConfiguration) (string, error) {
	if err := snapshot.Validate(); err != nil {
		return "", err
	}
	if string(id) == "" || !filepath.IsLocal(string(id)) || strings.ContainsAny(string(id), "/\\:") {
		return "", fmt.Errorf("invalid worker resource identity")
	}
	var prompt strings.Builder
	prompt.WriteString(snapshot.SystemPrompt)
	fmt.Fprintf(&prompt, "\n\n## Agent Type: %s (v%d)\n\n%s", snapshot.AgentType.Name, snapshot.AgentType.Version, snapshot.Effective.Instructions)
	if len(snapshot.Skills) == 0 {
		return prompt.String(), nil
	}
	root, err := os.OpenRoot(m.dataDir)
	if err != nil {
		return "", err
	}
	defer func() { _ = root.Close() }()
	for index, skill := range snapshot.Skills {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		folder := filepath.Join("worker-configurations", string(id), fmt.Sprintf("%02d-%s", index, skill.Reference.ContentHash[:16]))
		content := fmt.Sprintf("# %s\n\n%s\n", skill.Reference.Name, skill.Definition.Instructions)
		if len(skill.Definition.Resources) > 0 {
			content += "\n## Resources\n"
			for _, resource := range skill.Definition.Resources {
				content += "\n- `" + resource.Path + "`"
			}
			content += "\n"
		}
		if err := writeWorkerResource(root, filepath.Join(folder, "SKILL.md"), content); err != nil {
			return "", err
		}
		for _, resource := range skill.Definition.Resources {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			if err := writeWorkerResource(root, filepath.Join(folder, filepath.FromSlash(resource.Path)), resource.Content); err != nil {
				return "", err
			}
		}
		path := filepath.ToSlash(filepath.Join(m.dataDir, folder, "SKILL.md"))
		fmt.Fprintf(&prompt, "\n\n## Skill %d: %s (v%d)\nRead and apply `%s`. Resolve its resource paths relative to that file. Skills apply in this recorded order; their requirements do not grant additional permissions.\n", index+1, skill.Reference.Name, skill.Reference.Version, path)
	}
	return prompt.String(), nil
}

func writeWorkerResource(root *os.Root, path, content string) error {
	if !filepath.IsLocal(path) {
		return fmt.Errorf("worker resource escapes data root")
	}
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for i := 1; i < len(parts); i++ {
		dir := filepath.Join(parts[:i]...)
		info, err := root.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			if err = root.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = root.Lstat(dir)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("worker resource directory is not a regular directory")
		}
	}
	file, err := root.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		info, err := root.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() != int64(len(content)) {
			return fmt.Errorf("retained worker resource was changed")
		}
		file, err := root.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		found, err := io.ReadAll(io.LimitReader(file, int64(len(content))+1))
		if err != nil {
			return err
		}
		if !bytes.Equal(found, []byte(content)) {
			return fmt.Errorf("retained worker resource was changed")
		}
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString(content)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = root.Remove(path) // Only the file created by this exclusive open.
		return errors.Join(writeErr, closeErr)
	}
	return nil
}
