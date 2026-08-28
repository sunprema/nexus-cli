// Package paths resolves filesystem locations Nexus needs relative to the
// current git worktree.
package paths

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// worktreeRootCache caches the worktree root to avoid repeated git commands.
// The cache is keyed by the current working directory to handle directory changes.
var (
	worktreeRootMu       sync.RWMutex
	worktreeRootCache    string
	worktreeRootCacheDir string
)

// WorktreeRoot returns the git worktree root directory.
// Uses 'git rev-parse --show-toplevel' which returns the working tree toplevel.
// In a worktree this is the worktree root, not the main repository root.
// The result is cached per working directory.
// Returns an error if not inside a git repository.
func WorktreeRoot(ctx context.Context) (string, error) {
	// Get current working directory to check cache validity
	cwd, err := os.Getwd()
	if err != nil {
		cwd = ""
	}

	// Check cache with read lock first
	worktreeRootMu.RLock()
	if worktreeRootCache != "" && worktreeRootCacheDir == cwd {
		cached := worktreeRootCache
		worktreeRootMu.RUnlock()
		return cached, nil
	}
	worktreeRootMu.RUnlock()

	// Cache miss - get worktree root and update cache with write lock
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git worktree root: %w", err)
	}

	root := strings.TrimSpace(string(output))

	worktreeRootMu.Lock()
	worktreeRootCache = root
	worktreeRootCacheDir = cwd
	worktreeRootMu.Unlock()

	return root, nil
}

// ClearWorktreeRootCache clears the cached worktree root.
// This is primarily useful for testing when changing directories.
func ClearWorktreeRootCache() {
	worktreeRootMu.Lock()
	worktreeRootCache = ""
	worktreeRootCacheDir = ""
	worktreeRootMu.Unlock()
}
