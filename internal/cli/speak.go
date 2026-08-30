package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sunprema/nexus-cli/internal/paths"
)

// `nexus speak` reads an explainer entry aloud through the operating
// system's own text-to-speech engine (macOS `say`, Linux espeak/speech-
// dispatcher, Windows System.Speech). It deliberately has no browser or
// editor dependency: the whole point is that "read me the summary of
// auth.py" works from a bare terminal, and from any MCP-host agent via the
// nexus_speak tool in mcp.go, with nothing else installed.
//
// Only one thing speaks at a time. Starting a new utterance replaces the
// one in progress, and `nexus speak --stop` (or nexus_speak with stop=true)
// interrupts it — tracked through a small pid file in the OS temp dir, so
// a stop request from a different process (the MCP server vs. a shell)
// still finds the right speaker.

// nexusSpeakVoiceEnv names the environment variable that supplies a default
// voice when --voice isn't given. Voice choice is a per-person preference,
// not a per-repo one, which is why it isn't a .nexus/settings.json field.
const nexusSpeakVoiceEnv = "NEXUS_SPEAK_VOICE"

// nexusSpeakWordsPerSecond is the rough pace of desktop TTS engines at their
// default rate (~155 words per minute), used only for the duration estimate
// reported back to callers.
const nexusSpeakWordsPerSecond = 2.6

// nexusSpeakPreviewRunes bounds the text preview echoed back to an MCP
// caller, so a tool result stays small even for a long narrative.
const nexusSpeakPreviewRunes = 160

// nexusSpeakResult is the outcome of a speak request — what the MCP tool
// returns as JSON, and what the CLI summarises on stdout. Every ordinary
// outcome (no explainer entry, nothing to say, stopped nothing) is reported
// here rather than as a Go error; a returned error means the request
// itself couldn't be carried out (no TTS engine, spawn failure).
type nexusSpeakResult struct {
	// Spoke is true when a TTS process was started for this request.
	Spoke bool `json:"spoke"`
	// Stopped is true when a previous utterance was interrupted — either
	// by an explicit stop request or because this one replaced it.
	Stopped bool   `json:"stopped"`
	Engine  string `json:"engine,omitempty"`
	Path    string `json:"path,omitempty"`
	Mode    string `json:"mode,omitempty"`
	Words   int    `json:"words"`
	// EstimatedSeconds is a rough duration so a caller (or the agent
	// relaying to a person) knows whether to expect ten seconds or three
	// minutes of audio.
	EstimatedSeconds int    `json:"estimated_seconds"`
	Preview          string `json:"preview,omitempty"`
	// Text carries the full prepared text only when the caller asked for
	// it (--print / print=true) instead of audio.
	Text string `json:"text,omitempty"`
	// Error explains a "didn't speak" outcome that isn't a failure of the
	// request itself: Nexus not set up, no entry for this path yet, entry
	// has no summary. Empty whenever Spoke is true.
	Error string `json:"error,omitempty"`
}

// nexusSpeakRequest is what both the CLI command and the MCP tool build
// before handing off to runNexusSpeak, so the two surfaces can't drift in
// how they resolve text or pick an engine.
type nexusSpeakRequest struct {
	// Path names a code file whose explainer entry should be read. Ignored
	// when Text is set.
	Path string
	// Mode is "summary" (the one-sentence frontmatter gist) or "full" (the
	// whole narrative body). Empty means full.
	Mode string
	// Text is spoken verbatim (after the same markdown cleanup), bypassing
	// the explainer lookup — for an agent that has composed its own
	// reading, e.g. a tour's stops strung together.
	Text  string
	Voice string
	// Print returns the prepared text instead of speaking it.
	Print bool
	// Stop interrupts the current utterance and speaks nothing.
	Stop bool
	// Detach returns as soon as the TTS process has started rather than
	// waiting for it to finish. The MCP tool always detaches: a long
	// narrative must not block the agent for minutes. The CLI blocks so
	// Ctrl-C works the way a person expects.
	Detach bool
}

func newNexusSpeakCmd() *cobra.Command {
	var (
		summary   bool
		text      string
		voice     string
		printOnly bool
		stop      bool
	)

	cmd := &cobra.Command{
		Use:   "speak [<path> | --text <text> | -]",
		Short: "Read a file's explainer entry aloud",
		Long: `Read a code file's explainer entry aloud through the operating system's own
text-to-speech engine — macOS "say", Linux espeak-ng/espeak/spd-say, or
Windows System.Speech. Nothing else needs to be installed.

<path> is a code file path relative to the repo root (e.g. src/auth.py).
By default the whole narrative is read; --summary reads just the
one-sentence gist from the entry's frontmatter. Markdown syntax, code
blocks and desync marker lines are stripped first so the audio is prose,
not punctuation.

--text reads arbitrary text instead of an explainer entry; "-" as the
path reads text from stdin. --print shows what would be spoken without
speaking it. --stop interrupts whatever is currently being read.

Only one thing speaks at a time: a new "nexus speak" replaces the one in
progress. Set ` + nexusSpeakVoiceEnv + ` to pick a default voice for your machine
(the names your engine accepts, e.g. "say -v ?" on macOS).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req := nexusSpeakRequest{Voice: voice, Print: printOnly, Stop: stop, Text: text}
			if summary {
				req.Mode = "summary"
			}
			if len(args) == 1 {
				if args[0] == "-" {
					data, err := io.ReadAll(cmd.InOrStdin())
					if err != nil {
						return fmt.Errorf("read stdin: %w", err)
					}
					req.Text = string(data)
					if strings.TrimSpace(req.Text) == "" {
						return errors.New("nothing to speak: stdin was empty")
					}
				} else {
					req.Path = args[0]
				}
			}
			if !stop && req.Path == "" && req.Text == "" {
				return errors.New("nothing to speak: give a <path>, --text, or - for stdin")
			}
			return runNexusSpeakCLI(cmd, req)
		},
	}
	cmd.Flags().BoolVar(&summary, "summary", false, "Read only the entry's one-sentence summary")
	cmd.Flags().StringVar(&text, "text", "", "Read this text instead of an explainer entry")
	cmd.Flags().StringVar(&voice, "voice", "", "Voice name to use (default: $"+nexusSpeakVoiceEnv+", else the engine's default)")
	cmd.Flags().BoolVar(&printOnly, "print", false, "Print the prepared text instead of speaking it")
	cmd.Flags().BoolVar(&stop, "stop", false, "Stop whatever is currently being read")
	return cmd
}

func runNexusSpeakCLI(cmd *cobra.Command, req nexusSpeakRequest) error {
	out := cmd.OutOrStdout()
	result, err := runNexusSpeak(cmd.Context(), req)
	if err != nil {
		cmd.SilenceUsage = true
		return err
	}
	switch {
	case req.Print:
		fmt.Fprint(out, result.Text)
		if !strings.HasSuffix(result.Text, "\n") {
			fmt.Fprintln(out)
		}
	case result.Error != "":
		cmd.SilenceUsage = true
		fmt.Fprintln(cmd.ErrOrStderr(), result.Error)
		return NewSilentError(errors.New(result.Error))
	case req.Stop:
		if result.Stopped {
			fmt.Fprintln(out, "Stopped.")
		} else {
			fmt.Fprintln(out, "Nothing is being read.")
		}
	}
	return nil
}

// runNexusSpeak resolves the text for req, then either returns it (Print),
// stops the current utterance (Stop), or hands it to the OS engine. Shared
// by the CLI and the nexus_speak MCP tool.
func runNexusSpeak(ctx context.Context, req nexusSpeakRequest) (nexusSpeakResult, error) {
	if req.Stop {
		stopped, err := stopNexusSpeech()
		if err != nil {
			return nexusSpeakResult{}, err
		}
		return nexusSpeakResult{Stopped: stopped}, nil
	}

	mode := req.Mode
	if mode == "" {
		mode = "full"
	}
	if mode != "full" && mode != "summary" {
		return nexusSpeakResult{}, fmt.Errorf("unknown mode %q: want \"summary\" or \"full\"", req.Mode)
	}

	result := nexusSpeakResult{Path: req.Path, Mode: mode}
	var text string
	switch {
	case req.Text != "":
		text = nexusSpeakableText(req.Text)
		result.Path, result.Mode = "", ""
	default:
		var errText string
		text, errText = nexusSpeakTextForPath(ctx, req.Path, mode)
		if errText != "" {
			result.Error = errText
			return result, nil
		}
	}
	if strings.TrimSpace(text) == "" {
		result.Error = "nothing to speak: the text is empty after stripping markup"
		return result, nil
	}

	result.Words = len(strings.Fields(text))
	result.EstimatedSeconds = int(float64(result.Words)/nexusSpeakWordsPerSecond + 0.5)
	result.Preview = nexusSpeakPreview(text)
	if req.Print {
		result.Text = text
		return result, nil
	}

	engine, err := findTTSEngine()
	if err != nil {
		return nexusSpeakResult{}, err
	}
	result.Engine = engine.Name

	voice := req.Voice
	if voice == "" {
		voice = os.Getenv(nexusSpeakVoiceEnv)
	}

	// Replace whatever is currently playing rather than talking over it.
	if stopped, err := stopNexusSpeech(); err == nil {
		result.Stopped = stopped
	}

	if err := engine.speak(ctx, text, voice, req.Detach); err != nil {
		return nexusSpeakResult{}, err
	}
	result.Spoke = true
	return result, nil
}

// nexusSpeakTextForPath looks up path's explainer entry the same way
// `nexus show` does and returns the prose to read. A non-empty errText is
// an ordinary "nothing to read" explanation for the caller, not a failure.
func nexusSpeakTextForPath(ctx context.Context, path, mode string) (text, errText string) {
	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		return "", "Not a git repository. Run 'nexus speak' from inside a git repository, or use --text."
	}
	show, err := computeNexusShow(ctx, repoRoot, path)
	if err != nil {
		return "", err.Error()
	}
	if show.Error != "" {
		return "", show.Error
	}
	if !show.Found {
		return "", fmt.Sprintf("No explainer entry for %s yet. Narrate this commit to create one.", path)
	}

	if mode == "summary" {
		if show.Summary == "" {
			return "", fmt.Sprintf("The explainer entry for %s has no summary yet (it was narrated before summaries existed). Re-narrate it, or read the full entry instead.", path)
		}
		text = nexusSpeakableText(show.Summary)
	} else {
		_, body, _ := parseNexusFrontmatter(show.Content)
		text = nexusSpeakableText(body)
	}

	// A listener can't see the marker line, so say it up front instead of
	// silently dropping it with the rest of the markup.
	if show.Desynced {
		text = "Heads up: this explainer may be out of date with the code. " + text
	}
	return text, ""
}

func nexusSpeakPreview(text string) string {
	r := []rune(text)
	if len(r) <= nexusSpeakPreviewRunes {
		return text
	}
	return string(r[:nexusSpeakPreviewRunes]) + "…"
}

// --- Markdown → prose -------------------------------------------------------

var (
	nexusSpeakFencePattern     = regexp.MustCompile("(?s)(^|\n)(```|~~~)[^\n]*\n.*?\n(```|~~~)[ \t]*(\n|$)")
	nexusSpeakImagePattern     = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	nexusSpeakLinkPattern      = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	nexusSpeakHTMLTagPattern   = regexp.MustCompile(`</?[A-Za-z][^>]*>`)
	nexusSpeakHeadingPattern   = regexp.MustCompile(`^#{1,6}\s+`)
	nexusSpeakBulletPattern    = regexp.MustCompile(`^([-*+]|\d+[.)])\s+`)
	nexusSpeakTableRulePattern = regexp.MustCompile(`^\|?\s*:?-{2,}:?\s*(\|\s*:?-{2,}:?\s*)*\|?$`)
	nexusSpeakEmphasisPattern  = regexp.MustCompile(`\*\*|__|\*`)
	nexusSpeakSpacePattern     = regexp.MustCompile(`[ \t]{2,}`)
)

// nexusSpeakableText turns explainer Markdown into prose a TTS engine can
// read without narrating punctuation: code fences and mermaid diagrams are
// dropped, desync marker lines are removed (the caller announces desync in
// words instead), headings and list items become sentences, and inline
// markup (emphasis, links, backticks, HTML) is unwrapped. Frontmatter is
// NOT stripped here — callers that read a whole entry split it off first,
// while --text input has none.
func nexusSpeakableText(md string) string {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = nexusSpeakFencePattern.ReplaceAllString(md, "$1")

	var lines []string
	for line := range strings.SplitSeq(md, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			lines = append(lines, "")
			continue
		}
		if strings.HasPrefix(line, nexusDesyncMarker) || nexusSpeakTableRulePattern.MatchString(line) {
			continue
		}
		// Strip block-level prefixes: blockquote markers can nest ("> > "),
		// then a heading or list marker may follow.
		for strings.HasPrefix(line, ">") {
			line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
		}
		isHeading := nexusSpeakHeadingPattern.MatchString(line)
		line = nexusSpeakHeadingPattern.ReplaceAllString(line, "")
		isItem := nexusSpeakBulletPattern.MatchString(line)
		line = nexusSpeakBulletPattern.ReplaceAllString(line, "")
		if strings.Contains(line, "|") {
			line = strings.Trim(line, "|")
			line = strings.Join(strings.Fields(strings.ReplaceAll(line, "|", " , ")), " ")
			line = strings.ReplaceAll(line, " ,", ",")
		}

		line = nexusSpeakImagePattern.ReplaceAllString(line, "$1")
		line = nexusSpeakLinkPattern.ReplaceAllString(line, "$1")
		line = nexusSpeakHTMLTagPattern.ReplaceAllString(line, "")
		line = strings.ReplaceAll(line, "`", "")
		line = nexusSpeakEmphasisPattern.ReplaceAllString(line, "")
		line = strings.TrimSpace(nexusSpeakSpacePattern.ReplaceAllString(line, " "))
		if line == "" {
			continue
		}
		// A heading or bullet has no terminal punctuation of its own; give
		// the engine a sentence boundary so it pauses instead of running
		// the item into the next one.
		if (isHeading || isItem) && !strings.ContainsAny(line[len(line)-1:], ".!?:;") {
			line += "."
		}
		lines = append(lines, line)
	}

	// Collapse runs of blank lines into paragraph breaks and drop
	// leading/trailing ones.
	var out []string
	blank := true
	for _, l := range lines {
		if l == "" {
			if !blank {
				out = append(out, "")
			}
			blank = true
			continue
		}
		out = append(out, l)
		blank = false
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// --- OS text-to-speech engines ---------------------------------------------

// ttsEngine describes one way of speaking text: an executable plus the
// arguments that make it read from stdin, with an optional voice.
type ttsEngine struct {
	Name string
	// path is the resolved executable.
	path string
	// args builds the argument list; voice is "" when none was chosen.
	args func(voice string) []string
}

// lookPathTTS is exec.LookPath, replaceable in tests so engine selection
// can be exercised without any engine installed.
var lookPathTTS = exec.LookPath

// ttsEngineCandidates lists the engines to try, in order, for the current
// platform. Every one reads its text from stdin so a long narrative never
// hits an argument-length limit.
func ttsEngineCandidates() []ttsEngine {
	switch runtime.GOOS {
	case "darwin":
		return []ttsEngine{{
			Name: "say",
			args: func(voice string) []string {
				if voice != "" {
					return []string{"-v", voice}
				}
				return nil
			},
		}}
	case "windows":
		return []ttsEngine{{
			Name: "powershell",
			args: func(voice string) []string {
				script := "Add-Type -AssemblyName System.Speech; " +
					"$s = New-Object System.Speech.Synthesis.SpeechSynthesizer; "
				if voice != "" {
					script += "$s.SelectVoice('" + strings.ReplaceAll(voice, "'", "''") + "'); "
				}
				script += "$s.Speak([Console]::In.ReadToEnd())"
				return []string{"-NoProfile", "-NonInteractive", "-Command", script}
			},
		}}
	default:
		voiceArgs := func(flag string) func(string) []string {
			return func(voice string) []string {
				if voice != "" {
					return []string{flag, voice}
				}
				return nil
			}
		}
		return []ttsEngine{
			{Name: "espeak-ng", args: func(v string) []string { return append([]string{"--stdin"}, voiceArgs("-v")(v)...) }},
			{Name: "espeak", args: func(v string) []string { return append([]string{"--stdin"}, voiceArgs("-v")(v)...) }},
			// speech-dispatcher: -e pipes stdin through to the speaker.
			{Name: "spd-say", args: func(v string) []string { return append([]string{"-e"}, voiceArgs("-y")(v)...) }},
		}
	}
}

// findTTSEngine returns the first installed engine for this platform, or an
// error naming what to install when none is.
func findTTSEngine() (ttsEngine, error) {
	candidates := ttsEngineCandidates()
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c.Name)
		if p, err := lookPathTTS(c.Name); err == nil {
			c.path = p
			return c, nil
		}
	}
	return ttsEngine{}, fmt.Errorf("no text-to-speech engine found: install one of %s", strings.Join(names, ", "))
}

// speak starts the engine with text on stdin. When detach is false it
// waits for the engine to finish (so a terminal user's Ctrl-C lands on it);
// when true it returns as soon as the process is running and reaps it in
// the background. Either way the process is recorded in the pid file so a
// later stop request can find it.
func (e ttsEngine) speak(ctx context.Context, text, voice string, detach bool) error {
	var cmd *exec.Cmd
	if detach {
		// Deliberately not bound to ctx: the utterance should outlive the
		// tool call that started it.
		cmd = exec.Command(e.path, e.args(voice)...) //nolint:gosec,noctx // G204/noctx: path comes from LookPath over a fixed engine list; detached on purpose (see comment).
	} else {
		cmd = exec.CommandContext(ctx, e.path, e.args(voice)...) //nolint:gosec // G204: path comes from LookPath over a fixed engine list, args are built here.
	}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("open stdin for %s: %w", e.Name, err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", e.Name, err)
	}
	pid := cmd.Process.Pid
	writeErr := writeNexusSpeakPID(pid, e.Name)

	// Hand over the whole text before returning — even when detaching —
	// so a short-lived caller (an MCP host that exits right after the tool
	// call) can't truncate a long narrative mid-sentence. Explainer entries
	// are a few KB, far under the pipe buffer, so this doesn't block.
	if _, err := io.WriteString(stdin, text+"\n"); err != nil && !errors.Is(err, os.ErrClosed) {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		clearNexusSpeakPID(pid)
		return fmt.Errorf("send text to %s: %w", e.Name, err)
	}
	_ = stdin.Close()

	if detach {
		go func() {
			_ = cmd.Wait()
			clearNexusSpeakPID(pid)
		}()
		return writeErr
	}
	err = cmd.Wait()
	clearNexusSpeakPID(pid)
	if err != nil {
		// Being killed by a concurrent --stop (or Ctrl-C) is the expected
		// way an utterance ends early, not a failure to report: a process
		// ended by a signal has ExitCode -1. A real non-zero exit (bad
		// voice name, engine misconfigured) is still surfaced.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() != -1 && ctx.Err() == nil {
			return fmt.Errorf("%s exited: %w", e.Name, err)
		}
	}
	return writeErr
}

// --- Current-speaker tracking ----------------------------------------------

// nexusSpeakPIDFile records the running TTS process (pid and engine name)
// so "stop" — from any process — can find it. It lives in the OS temp dir,
// not the repo, because there's one speaker per machine, not per repo.
func nexusSpeakPIDFile() string {
	return filepath.Join(os.TempDir(), "nexus-speak.pid")
}

func writeNexusSpeakPID(pid int, engine string) error {
	if err := os.WriteFile(nexusSpeakPIDFile(), []byte(strconv.Itoa(pid)+"\n"+engine+"\n"), 0o600); err != nil {
		return fmt.Errorf("record speaker pid: %w", err)
	}
	return nil
}

// clearNexusSpeakPID removes the pid file only if it still names pid, so a
// finished utterance never erases the record of a newer one that replaced it.
func clearNexusSpeakPID(pid int) {
	cur, _, ok := readNexusSpeakPID()
	if ok && cur == pid {
		_ = os.Remove(nexusSpeakPIDFile())
	}
}

func readNexusSpeakPID() (pid int, engine string, ok bool) {
	data, err := os.ReadFile(nexusSpeakPIDFile())
	if err != nil {
		return 0, "", false
	}
	parts := strings.SplitN(strings.TrimSpace(string(data)), "\n", 2)
	pid, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || pid <= 0 {
		return 0, "", false
	}
	if len(parts) == 2 {
		engine = strings.TrimSpace(parts[1])
	}
	return pid, engine, true
}

// stopNexusSpeech kills the recorded TTS process, if it's still the process
// we started. stopped is false when nothing was playing (or the record was
// stale). A pid can be reused by an unrelated process after ours exits, so
// on Unix the pid's command name is checked against the recorded engine
// before anything is signalled.
func stopNexusSpeech() (stopped bool, err error) {
	pid, engine, ok := readNexusSpeakPID()
	if !ok {
		return false, nil
	}
	if !nexusSpeakProcessMatches(pid, engine) {
		_ = os.Remove(nexusSpeakPIDFile())
		return false, nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(nexusSpeakPIDFile())
		return false, nil
	}
	if err := proc.Kill(); err != nil {
		_ = os.Remove(nexusSpeakPIDFile())
		if errors.Is(err, os.ErrProcessDone) {
			return false, nil
		}
		return false, fmt.Errorf("stop %s (pid %d): %w", engine, pid, err)
	}
	_ = os.Remove(nexusSpeakPIDFile())
	return true, nil
}

// nexusSpeakProcessMatches reports whether pid is still running the engine
// we recorded. On Unix it asks ps; on Windows (no ps) it trusts the record.
func nexusSpeakProcessMatches(pid int, engine string) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output() //nolint:gosec,noctx // G204: fixed ps invocation, pid is an integer we formatted; instant local query, no cancellation needed.
	if err != nil {
		return false
	}
	comm := strings.TrimSpace(string(out))
	if comm == "" {
		return false
	}
	// ps may print a full path (macOS) or just the basename (Linux).
	return engine != "" && filepath.Base(comm) == engine
}
