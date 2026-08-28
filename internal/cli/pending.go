package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sunprema/nexus-cli/internal/jsonutil"
)

// nexusPendingFile queues commits whose explainer narration hasn't run yet.
// Written by the post-commit hook (cheap, no LLM); drained by the 'narrate'
// skill and inspected by 'nexus sync'. Local/transient state — listed in
// .nexus/.gitignore, not committed. See
// docs/adr/0001-async-post-commit-narration-trigger.md.
const nexusPendingFile = nexusDir + "/pending.json"

// NexusPendingEntry is one commit awaiting explainer narration.
type NexusPendingEntry struct {
	Commit     string    `json:"commit"`
	RecordedAt time.Time `json:"recorded_at"`
}

type nexusPendingQueue struct {
	Pending []NexusPendingEntry `json:"pending"`
}

func loadNexusPending(repoRoot string) (nexusPendingQueue, error) {
	data, err := os.ReadFile(filepath.Join(repoRoot, nexusPendingFile)) //nolint:gosec // repoRoot + fixed suffix
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nexusPendingQueue{}, nil
		}
		return nexusPendingQueue{}, fmt.Errorf("read %s: %w", nexusPendingFile, err)
	}
	if len(data) == 0 {
		return nexusPendingQueue{}, nil
	}
	var q nexusPendingQueue
	if err := json.Unmarshal(data, &q); err != nil {
		return nexusPendingQueue{}, fmt.Errorf("parse %s: %w", nexusPendingFile, err)
	}
	return q, nil
}

func saveNexusPending(repoRoot string, q nexusPendingQueue) error {
	path := filepath.Join(repoRoot, nexusPendingFile)
	data, err := jsonutil.MarshalIndentWithNewline(q, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", nexusPendingFile, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", nexusDir, err)
	}
	return jsonutil.WriteFileAtomic(path, data, 0o644)
}

// appendNexusPending records commit as needing narration, unless it's
// already queued.
func appendNexusPending(repoRoot, commit string, now time.Time) error {
	q, err := loadNexusPending(repoRoot)
	if err != nil {
		return err
	}
	for _, e := range q.Pending {
		if e.Commit == commit {
			return nil
		}
	}
	q.Pending = append(q.Pending, NexusPendingEntry{Commit: commit, RecordedAt: now})
	return saveNexusPending(repoRoot, q)
}

// removeNexusPending drops commit from the queue, if present, and reports
// whether it was found there.
func removeNexusPending(repoRoot, commit string) (found bool, err error) {
	q, err := loadNexusPending(repoRoot)
	if err != nil {
		return false, err
	}
	kept := q.Pending[:0]
	for _, e := range q.Pending {
		if e.Commit == commit {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if !found {
		return false, nil
	}
	q.Pending = kept
	if err := saveNexusPending(repoRoot, q); err != nil {
		return false, err
	}
	return true, nil
}
