package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// nexusHookMarker identifies a post-commit hook that already has Nexus's
// line installed, so re-running `nexus init` is idempotent.
const nexusHookMarker = "Nexus post-commit hook"

// nexusPostCommitHookLine invokes the hidden hook entrypoint. It is written
// to never fail: post-commit cannot abort a commit (the commit object
// already exists by the time it runs), so a missing or broken `nexus`
// binary here should be silent rather than print a warning on every commit.
const nexusPostCommitHookLine = `command -v nexus >/dev/null 2>&1 && nexus __post-commit-hook >/dev/null 2>&1 || true`

// posixShellShebangs are the shebangs ensureNexusPostCommitHook will safely
// append a POSIX shell line after. Anything else (a Python/Ruby/Node hook,
// or no shebang at all) is left untouched — appending shell syntax to a
// non-shell interpreter would corrupt it.
var posixShellShebangs = []string{"#!/bin/sh", "#!/bin/bash", "#!/usr/bin/env sh", "#!/usr/bin/env bash"}

// ensureNexusPostCommitHook installs, or extends, the repository's
// post-commit hook so every commit gets queued for explainer narration (see
// docs/adr/0001-async-post-commit-narration-trigger.md — the hook only
// enqueues; it never calls an LLM itself).
//
// Never backs up and replaces an existing hook: post-commit can't gate a
// commit either way, so appending our line is always safe, and a repo may
// share this hook with other tooling that also wants a say in post-commit.
//
// Returns (installed, warning, err). installed is true only when Nexus's
// line was newly added. warning is set (installed=false, err=nil) when the
// hook exists but its interpreter isn't one we can safely append shell
// syntax to — the caller should show it to the user rather than silently
// doing nothing.
func ensureNexusPostCommitHook(hooksDir string) (installed bool, warning string, err error) {
	path := filepath.Join(hooksDir, "post-commit")

	existing, readErr := os.ReadFile(path) //nolint:gosec // path is hooksDir + fixed hook name
	if readErr != nil && !os.IsNotExist(readErr) {
		return false, "", fmt.Errorf("read existing post-commit hook: %w", readErr)
	}

	if strings.Contains(string(existing), nexusHookMarker) {
		return false, "", nil
	}

	if err := os.MkdirAll(hooksDir, 0o755); err != nil { //nolint:gosec // git hooks require executable permissions
		return false, "", fmt.Errorf("create hooks directory: %w", err)
	}

	addition := fmt.Sprintf("\n# %s\n%s\n", nexusHookMarker, nexusPostCommitHookLine)

	if len(existing) == 0 {
		content := "#!/bin/sh" + addition
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil { //nolint:gosec // git hooks require executable permissions
			return false, "", fmt.Errorf("write post-commit hook: %w", err)
		}
		return true, "", nil
	}

	if !hasPosixShellShebang(string(existing)) {
		return false, fmt.Sprintf(
			"! Found an existing post-commit hook with an interpreter Nexus doesn't recognize (%s).\n"+
				"  Add this line to it manually to enable explainer narration:\n"+
				"    %s\n", path, nexusPostCommitHookLine,
		), nil
	}

	content := strings.TrimRight(string(existing), "\n") + "\n" + addition
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil { //nolint:gosec // git hooks require executable permissions
		return false, "", fmt.Errorf("write post-commit hook: %w", err)
	}
	return true, "", nil
}

func hasPosixShellShebang(content string) bool {
	firstLine, _, _ := strings.Cut(content, "\n")
	firstLine = strings.TrimSpace(firstLine)
	for _, shebang := range posixShellShebangs {
		if firstLine == shebang {
			return true
		}
	}
	return false
}
