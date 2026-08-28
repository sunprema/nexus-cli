package cli

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitHooksDir returns the git hooks directory for the current repository. It
// delegates to `git rev-parse --git-path hooks` to leverage git's own
// resolution (worktrees, GIT_DIR overrides, etc.) rather than reimplementing
// it. Unlike Entire's strategy.GetHooksDir, this doesn't cache the result —
// Nexus is a one-shot CLI process, so there's nothing to amortize the cache
// against.
func gitHooksDir(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-path", "hooks")
	output, err := cmd.Output()
	if err != nil {
		return "", errors.New("not a git repository")
	}

	hooksDir := strings.TrimSpace(string(output))
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(".", hooksDir)
	}

	return filepath.Clean(hooksDir), nil
}
