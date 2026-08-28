package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sunprema/nexus-cli/internal/jsonutil"
)

// nexusIgnoreFile lives at the repo root, like .gitignore, rather than under
// .nexus/ — same reasoning as .gitignore itself: it's most discoverable
// sitting alongside the code it governs, not nested in a tool's config dir.
const nexusIgnoreFile = ".nexusignore"

// defaultNexusIgnore ships with lockfiles and common test-file patterns
// pre-enabled. Test files are opinionated but deliberate — see
// docs/adr/0003-nexusignore.md: a test's own name and assertions already
// say what it checks, so narrating it duplicates that for comparatively
// little reviewer value. Teams that want test changes to show up in
// 'explainer' can just delete those lines.
const defaultNexusIgnore = `# Nexus ignore file
#
# Gitignore-style patterns for paths the 'narrate' skill skips when writing
# explainer entries. Team-shared: commit this alongside .nexus/. Edit
# freely — add, remove, or comment out any line.
#
# Structural exclusions (.nexus/, .git*) are NOT listed here: those are
# hardcoded in the 'narrate' skill and can't be overridden, since Nexus
# narrating its own config would be nonsensical.

# Lockfiles: machine-generated, nothing authorial to narrate.
package-lock.json
pnpm-lock.yaml
yarn.lock
go.sum
Cargo.lock
Gemfile.lock
poetry.lock
composer.lock

# Test files: a test's own name and assertions already say what it checks;
# narrating that duplicates it for comparatively little reviewer value.
# Remove or comment out any of these to have test changes appear in
# 'explainer' too.
*_test.go
*.test.ts
*.test.tsx
*.test.js
*.test.jsx
*_test.py
test_*.py
*_spec.rb
*.spec.ts
*.spec.js
**/test/**
**/tests/**
**/__tests__/**
**/spec/**
`

func ensureNexusIgnore(repoRoot string) (created bool, err error) {
	path := filepath.Join(repoRoot, nexusIgnoreFile)
	if _, statErr := os.Stat(path); statErr == nil {
		return false, nil
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return false, statErr
	}
	if err := jsonutil.WriteFileAtomic(path, []byte(defaultNexusIgnore), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", nexusIgnoreFile, err)
	}
	return true, nil
}
