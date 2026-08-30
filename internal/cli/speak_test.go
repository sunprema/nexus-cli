package cli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// nexusSpeakableText must turn explainer Markdown into prose a TTS engine
// reads cleanly: no fences, no marker lines, no inline markup, and sentence
// boundaries after headings and bullets so the engine pauses.
func TestSpeakableText_StripsMarkdown(t *testing.T) {
	t.Parallel()
	md := "# Title\n\n" +
		nexusDesyncMarker + " something changed\n\n" +
		"Some **bold** and *italic* and `code` text with a [link](https://x.y/z).\n" +
		"An ![image](pic.png) too.\n\n" +
		"```go\nfunc secret() {}\n```\n\n" +
		"```mermaid\ngraph TD; A-->B\n```\n\n" +
		"## How it works\n" +
		"- first point\n" +
		"- second point!\n" +
		"1. numbered\n\n" +
		"> quoted line\n\n" +
		"| col a | col b |\n|---|---|\n| one | two |\n\n" +
		"<b>html</b> tags gone.\n"

	got := nexusSpeakableText(md)

	for _, banned := range []string{"#", "**", "`", "```", "func secret", "graph TD", nexusDesyncMarker, "https://", "](", "<b>", "|", "- first", "1. numbered"} {
		if strings.Contains(got, banned) {
			t.Errorf("expected %q to be stripped, got:\n%s", banned, got)
		}
	}
	for _, want := range []string{"Title.", "Some bold and italic and code text with a link.", "image too.", "How it works.", "first point.", "second point!", "numbered.", "quoted line", "col a, col b", "one, two", "html tags gone."} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in output, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("runs of blank lines should collapse, got:\n%s", got)
	}
}

func TestSpeakableText_KeepsPlainProse(t *testing.T) {
	t.Parallel()
	in := "Just a sentence. Another one with source_commit inside."
	if got := nexusSpeakableText(in); got != in {
		t.Errorf("plain prose should pass through unchanged, got %q", got)
	}
}

// Print mode must resolve and prepare text without touching any engine —
// it's how a caller checks what would be read on a machine with no TTS.
func TestRunNexusSpeak_PrintNeedsNoEngine(t *testing.T) {
	withNoTTSEngine(t)
	res, err := runNexusSpeak(context.Background(), nexusSpeakRequest{Text: "Hello **there**.", Print: true})
	if err != nil {
		t.Fatalf("print mode should not need an engine: %v", err)
	}
	if res.Text != "Hello there." || res.Spoke || res.Words != 2 || res.EstimatedSeconds != 1 {
		t.Errorf("unexpected result %+v", res)
	}
}

func TestRunNexusSpeak_NoEngineIsError(t *testing.T) {
	withNoTTSEngine(t)
	_, err := runNexusSpeak(context.Background(), nexusSpeakRequest{Text: "Hello."})
	if err == nil || !strings.Contains(err.Error(), "no text-to-speech engine") {
		t.Fatalf("expected a no-engine error, got %v", err)
	}
}

func TestRunNexusSpeak_BadModeIsError(t *testing.T) {
	t.Parallel()
	_, err := runNexusSpeak(context.Background(), nexusSpeakRequest{Path: "x.go", Mode: "loud", Print: true})
	if err == nil || !strings.Contains(err.Error(), `unknown mode "loud"`) {
		t.Fatalf("expected an unknown-mode error, got %v", err)
	}
}

func TestRunNexusSpeak_EmptyAfterStripIsOrdinaryResult(t *testing.T) {
	t.Parallel()
	res, err := runNexusSpeak(context.Background(), nexusSpeakRequest{Text: "```\nonly code\n```", Print: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Spoke || res.Error == "" {
		t.Errorf("code-only input should report nothing to speak, got %+v", res)
	}
}

// This repo is narrated on itself, so the summary lookup runs against real
// explainer state — shape only, since the summary text changes over time.
func TestRunNexusSpeak_SummaryFromRealExplainer(t *testing.T) {
	t.Parallel()
	res, err := runNexusSpeak(context.Background(), nexusSpeakRequest{Path: "internal/cli/show.go", Mode: "summary", Print: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Skipf("explainer state unavailable here: %s", res.Error)
	}
	if res.Text == "" || res.Words == 0 || strings.Contains(res.Text, "---") {
		t.Errorf("expected a frontmatter-free summary, got %+v", res)
	}
	if res.Path != "internal/cli/show.go" || res.Mode != "summary" {
		t.Errorf("expected path/mode echoed back, got %+v", res)
	}
}

func TestRunNexusSpeak_MissingEntryIsOrdinaryResult(t *testing.T) {
	t.Parallel()
	res, err := runNexusSpeak(context.Background(), nexusSpeakRequest{Path: "definitely/not/here.go", Print: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Spoke || res.Error == "" {
		t.Errorf("a missing entry should be reported in Error, not spoken, got %+v", res)
	}
}

// The engine list is per-platform and each entry must resolve through
// LookPath; with nothing installed the error names what to install.
func TestFindTTSEngine_PicksFirstInstalled(t *testing.T) {
	candidates := ttsEngineCandidates()
	if len(candidates) == 0 {
		t.Fatalf("no engine candidates for %s", runtime.GOOS)
	}
	last := candidates[len(candidates)-1].Name
	orig := lookPathTTS
	t.Cleanup(func() { lookPathTTS = orig })
	lookPathTTS = func(name string) (string, error) {
		if name == last {
			return "/fake/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	}
	e, err := findTTSEngine()
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != last || e.path != "/fake/bin/"+last {
		t.Errorf("expected %s resolved, got %+v", last, e)
	}
	if args := e.args("Zoe"); !strings.Contains(strings.Join(args, " "), "Zoe") {
		t.Errorf("voice should reach the engine args, got %v", args)
	}
}

// A real detached utterance: a fake engine that records what it was given
// stands in for say/espeak, and the pid file must track it and clear once
// it exits — the mechanism stop relies on.
func TestTTSEngine_SpeakDetachedAndStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh as the fake engine")
	}
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	if got := filepath.Dir(nexusSpeakPIDFile()); filepath.Clean(got) != filepath.Clean(dir) {
		t.Skipf("os.TempDir does not honour TMPDIR here (%s), cannot isolate the pid file", got)
	}
	captured := filepath.Join(dir, "spoken.txt")
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	engine := ttsEngine{Name: "sh", path: sh, args: func(string) []string {
		return []string{"-c", "cat > " + captured + "; sleep 30"}
	}}

	if err := engine.speak(context.Background(), "spoken words", "", true); err != nil {
		t.Fatalf("speak: %v", err)
	}
	pid, name, ok := readNexusSpeakPID()
	if !ok || pid <= 0 || name != "sh" {
		t.Fatalf("expected pid file to record the fake engine, got pid=%d name=%q ok=%v", pid, name, ok)
	}
	// Text was handed over (written and stdin closed) before speak
	// returned; the fake engine still needs a moment to drain the pipe.
	var data []byte
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if data, err = os.ReadFile(captured); err == nil && strings.TrimSpace(string(data)) == "spoken words" {
			break
		}
	}
	if strings.TrimSpace(string(data)) != "spoken words" {
		t.Fatalf("expected the engine to have received the text, got %q (%v)", data, err)
	}

	stopped, err := stopNexusSpeech()
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !stopped {
		t.Fatal("expected stop to report it interrupted the speaker")
	}
	if _, err := os.Stat(nexusSpeakPIDFile()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("pid file should be removed after stop, stat err=%v", err)
	}
	if stopped, _ := stopNexusSpeech(); stopped {
		t.Error("a second stop should find nothing to stop")
	}
}

// A stale pid file whose pid now belongs to something else must never be
// signalled: the engine-name check refuses it and the record is discarded.
func TestStopNexusSpeech_IgnoresStaleRecord(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("engine-name check is Unix-only")
	}
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	if got := filepath.Dir(nexusSpeakPIDFile()); filepath.Clean(got) != filepath.Clean(dir) {
		t.Skipf("os.TempDir does not honour TMPDIR here (%s)", got)
	}
	// Our own pid is certainly alive and certainly not a TTS engine.
	if err := writeNexusSpeakPID(os.Getpid(), "say"); err != nil {
		t.Fatal(err)
	}
	stopped, err := stopNexusSpeech()
	if err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("must not report stopping a process that isn't the recorded engine")
	}
	if _, err := os.Stat(nexusSpeakPIDFile()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale pid file should be discarded, stat err=%v", err)
	}
}

func withNoTTSEngine(t *testing.T) {
	t.Helper()
	orig := lookPathTTS
	t.Cleanup(func() { lookPathTTS = orig })
	lookPathTTS = func(string) (string, error) { return "", exec.ErrNotFound }
}
