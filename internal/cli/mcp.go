package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/sunprema/nexus-cli/internal/paths"
	"github.com/sunprema/nexus-cli/internal/versioninfo"
)

// `nexus mcp` runs a Model Context Protocol (MCP) server over stdio so that
// MCP-host agents can reach a repo's explainer data (per-file narratives and
// guided tours) as MCP tools, instead of shelling out to `nexus show`/`nexus
// map`/`nexus tour` themselves. Transport is newline-delimited JSON-RPC 2.0
// (the MCP stdio framing).

// mcpProtocolVersion is the MCP revision we advertise when a client doesn't
// request one. We echo the client's requested version when present.
const mcpProtocolVersion = "2025-06-18"

// maxMCPMessageBytes bounds a single newline-delimited JSON-RPC message so a
// malformed or abusive line can't exhaust memory. 1 MiB is far above any real
// explainer/map/tour request.
const maxMCPMessageBytes = 1 << 20

// mcpServerName is the MCP serverInfo name advertised at initialize.
const mcpServerName = "nexus"

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Run a Model Context Protocol server for MCP-host agents",
		Long: `Runs a Model Context Protocol (MCP) server over stdio. Configure an MCP host to
launch "nexus mcp" as a stdio server; it exposes explainer entries and guided
tours as MCP tools so agents can read them without shelling out to nexus
directly. Read-only; speaks newline-delimited JSON-RPC 2.0.`,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return runMCPServer(c.Context(), c.InOrStdin(), c.OutOrStdout())
		},
	}
}

type mcpRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpError       `json:"error,omitempty"`
}

type mcpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// runMCPServer reads newline-delimited JSON-RPC 2.0 messages from in (one per
// line — the MCP stdio framing), bounding each line to maxMCPMessageBytes, and
// writes responses to out until EOF. Notifications (messages with no id) get no
// response, per JSON-RPC. An unparseable line — including a JSON-RPC batch array,
// which this server does not support — yields a parse error and the server
// recovers to the next line rather than terminating.
func runMCPServer(ctx context.Context, in io.Reader, out io.Writer) error {
	enc := json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), maxMCPMessageBytes)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}

		var req mcpRequest
		if err := json.Unmarshal(line, &req); err != nil {
			if encErr := enc.Encode(mcpResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &mcpError{Code: -32700, Message: "parse error"}}); encErr != nil {
				return fmt.Errorf("write mcp parse-error response: %w", encErr)
			}
			continue
		}

		// Reject a parseable-but-invalid request (missing/incorrect jsonrpc version
		// or empty method) with -32600 before dispatch, per JSON-RPC, rather than
		// treating it as method-not-found.
		if req.JSONRPC != "2.0" || req.Method == "" {
			id := req.ID
			if len(id) == 0 {
				id = json.RawMessage("null")
			}
			if encErr := enc.Encode(mcpResponse{JSONRPC: "2.0", ID: id, Error: &mcpError{Code: -32600, Message: "invalid request"}}); encErr != nil {
				return fmt.Errorf("write mcp invalid-request response: %w", encErr)
			}
			continue
		}

		result, rpcErr := dispatchMCP(ctx, req.Method, req.Params)

		// A request without an id is a notification: never responded to.
		if len(req.ID) == 0 {
			continue
		}

		resp := mcpResponse{JSONRPC: "2.0", ID: req.ID}
		if rpcErr != nil {
			resp.Error = rpcErr
		} else {
			resp.Result = result
		}
		if err := enc.Encode(resp); err != nil {
			return fmt.Errorf("write mcp response: %w", err)
		}
	}
	if err := sc.Err(); err != nil {
		// A single line exceeded maxMCPMessageBytes (the scanner can't resynchronize
		// past an over-long token) or the read failed; report once and stop.
		if errors.Is(err, bufio.ErrTooLong) {
			if encErr := enc.Encode(mcpResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &mcpError{Code: -32600, Message: "request too large"}}); encErr != nil {
				return fmt.Errorf("write mcp oversize response: %w", encErr)
			}
			return nil
		}
		return fmt.Errorf("read mcp request: %w", err)
	}
	return nil
}

func dispatchMCP(ctx context.Context, method string, params json.RawMessage) (any, *mcpError) {
	switch method {
	case "initialize":
		return mcpInitializeResult(params), nil
	case "notifications/initialized":
		return nil, nil // notification; result is ignored by the caller
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpToolDefs()}, nil
	case "tools/call":
		return handleMCPToolCall(ctx, params)
	default:
		return nil, &mcpError{Code: -32601, Message: "method not found: " + method}
	}
}

func mcpInitializeResult(params json.RawMessage) map[string]any {
	protocol := mcpProtocolVersion
	if len(params) > 0 {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(params, &p) == nil && p.ProtocolVersion != "" {
			protocol = p.ProtocolVersion
		}
	}
	return map[string]any{
		"protocolVersion": protocol,
		"capabilities":    map[string]any{"tools": map[string]any{}},
		"serverInfo":      map[string]any{"name": mcpServerName, "version": versioninfo.Version},
	}
}

func mcpToolDefs() []map[string]any {
	objSchema := func(props map[string]any) map[string]any {
		return map[string]any{"type": "object", "properties": props}
	}
	return []map[string]any{
		{
			"name":        "nexus_explainer",
			"description": "Get Project Nexus's explainer-branch narrative for a code file — a short, human-written summary of why the file is the way it is and how its logic flows, kept in sync with the code. Call this BEFORE reading or editing a file's raw code when Nexus is set up in this repo: the explainer often already captures the intent and edge cases you'd otherwise have to re-derive from the diff or commit history. Returns JSON: 'summary' is a one-sentence gist if you just need the file's purpose, not the full 'content'; found=false means no entry yet (nothing wrong, just not narrated); desynced=true means a prior check found the explainer disagreeing with the code — treat the code as authoritative and don't fully trust that entry's claims until it's re-narrated. For a test file, 'tests' lists each test function by name with what it actually verifies (not its assertions) — call this BEFORE editing or deleting a test so you know what invariant you'd be breaking, not just what it asserts.",
			"inputSchema": objSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Code file path, relative to the repo root (e.g. \"src/auth.py\").",
				},
			}),
		},
		{
			"name":        "nexus_map",
			"description": "Get a one-line index of every narrated file AND every guided tour in this repo — for files: path, one-sentence summary, desync status; for tours: slug, title, stop count (entries carry \"kind\": \"explainer\" or \"tour\") — for everything at once, no LLM call, instant. Call this FIRST when orienting in an unfamiliar repo that has Nexus set up, before reading any individual file's code: it's the cheapest way to learn what the codebase contains, whether a guided tour already exists for the area you care about, and which specific files are actually worth reading in depth. A file with an empty summary was narrated before summaries existed — nothing wrong, it just hasn't been re-narrated since; an item not listed at all simply doesn't exist yet. Fetch a tour's full stop list with nexus_tour; a file's full narrative with nexus_explainer.",
			"inputSchema": objSchema(map[string]any{}),
		},
		{
			"name":        "nexus_tour",
			"description": "Get a guided tour: an ordered list of files worth visiting to understand one area of this codebase, each with a short note on why it matters — meant for onboarding, not a substitute for nexus_explainer's per-file depth. Look up available tours (and their slugs) via nexus_map first. Returns JSON: found=false means no tour with this slug exists.",
			"inputSchema": objSchema(map[string]any{
				"slug": map[string]any{
					"type":        "string",
					"description": "Tour slug, as listed by nexus_map (e.g. \"request-lifecycle\").",
				},
			}),
		},
		{
			"name":        "nexus_speak",
			"description": "Read an explainer entry ALOUD to the person through their computer's own text-to-speech (macOS say, Linux espeak, Windows System.Speech) — for when they ask to \"read me\", \"tell me\", or \"listen to\" a file's summary or story instead of reading it on screen. Give either 'path' (a code file; mode \"summary\" for the one-sentence gist, \"full\" for the whole narrative) or 'text' (anything you've composed yourself, e.g. a tour's stops strung together, or your own answer to their question — markdown is stripped before speaking). Returns as soon as audio starts, not when it finishes: 'estimated_seconds' tells you how long it will play, so don't call again with new text until it's done unless the person asks to skip ahead, and DON'T repeat the spoken text in your reply — a one-line confirmation is enough. Only one thing speaks at a time; a new call replaces the current one, and stop=true interrupts it (\"stop reading\"). Returns JSON: spoke=false with 'error' means nothing was read (no entry yet, no summary yet, Nexus not set up) — relay the reason. A tool-level error means no speech engine could be found on this machine.",
			"inputSchema": objSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Code file path, relative to the repo root, whose explainer entry to read (e.g. \"src/auth.py\"). Ignored when 'text' is given.",
				},
				"mode": map[string]any{
					"type":        "string",
					"enum":        []string{"summary", "full"},
					"description": "With 'path': \"summary\" reads the one-sentence gist, \"full\" (default) the whole narrative.",
				},
				"text": map[string]any{
					"type":        "string",
					"description": "Text to read verbatim instead of an explainer entry. Markdown is stripped first.",
				},
				"voice": map[string]any{
					"type":        "string",
					"description": "Optional voice name for the engine (e.g. \"Samantha\" on macOS). Only pass one if the person asked for it; otherwise their NEXUS_SPEAK_VOICE default applies.",
				},
				"stop": map[string]any{
					"type":        "boolean",
					"description": "true stops whatever is currently being read and speaks nothing else.",
				},
				"print": map[string]any{
					"type":        "boolean",
					"description": "true returns the prepared text in 'text' instead of speaking it — for checking what would be read.",
				},
			}),
		},
	}
}

func handleMCPToolCall(ctx context.Context, params json.RawMessage) (any, *mcpError) {
	var call struct {
		Name      string `json:"name"`
		Arguments struct {
			Path  string `json:"path"`
			Slug  string `json:"slug"`
			Mode  string `json:"mode"`
			Text  string `json:"text"`
			Voice string `json:"voice"`
			Stop  bool   `json:"stop"`
			Print bool   `json:"print"`
		} `json:"arguments"`
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &call); err != nil {
			return nil, &mcpError{Code: -32602, Message: "invalid tool call params"}
		}
	}

	if call.Name == "" {
		return nil, &mcpError{Code: -32602, Message: "invalid params: tool name required"}
	}
	switch call.Name {
	case "nexus_explainer":
		return handleNexusExplainerTool(ctx, call.Arguments.Path)
	case "nexus_map":
		return handleNexusMapTool(ctx)
	case "nexus_tour":
		return handleNexusTourTool(ctx, call.Arguments.Slug)
	case "nexus_speak":
		return handleNexusSpeakTool(ctx, nexusSpeakRequest{
			Path:   call.Arguments.Path,
			Mode:   call.Arguments.Mode,
			Text:   call.Arguments.Text,
			Voice:  call.Arguments.Voice,
			Stop:   call.Arguments.Stop,
			Print:  call.Arguments.Print,
			Detach: true,
		})
	default:
		return nil, &mcpError{Code: -32602, Message: "unknown tool: " + call.Name}
	}
}

// handleNexusExplainerTool serves the nexus_explainer MCP tool. It shares
// computeNexusShow (show.go) with `nexus show`, so an MCP client and a shell
// script get identical path-mapping and branch resolution. Unlike a
// protocol-level failure, its "not found" and "not desynced" outcomes are
// ordinary results, not tool errors — only actually failing to resolve a
// repo or read a blob is treated as an error here.
func handleNexusExplainerTool(ctx context.Context, path string) (any, *mcpError) {
	if path == "" {
		return mcpToolErrorResult("invalid params: path is required"), nil
	}

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		return mcpToolErrorResult("not a git repository"), nil
	}

	result, err := computeNexusShow(ctx, repoRoot, path)
	if err != nil {
		return mcpToolErrorResult(err.Error()), nil
	}

	data, err := json.Marshal(result)
	if err != nil {
		return mcpToolErrorResult(fmt.Sprintf("encode result: %v", err)), nil
	}
	return mcpToolTextResult(string(data)), nil
}

// handleNexusMapTool serves the nexus_map MCP tool, sharing computeNexusMap
// (map.go) with `nexus map` for the same drift-proofing reason as
// handleNexusExplainerTool.
func handleNexusMapTool(ctx context.Context) (any, *mcpError) {
	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		return mcpToolErrorResult("not a git repository"), nil
	}

	result, err := computeNexusMap(ctx, repoRoot)
	if err != nil {
		return mcpToolErrorResult(err.Error()), nil
	}

	data, err := json.Marshal(result)
	if err != nil {
		return mcpToolErrorResult(fmt.Sprintf("encode result: %v", err)), nil
	}
	return mcpToolTextResult(string(data)), nil
}

// handleNexusTourTool serves the nexus_tour MCP tool. It shares
// computeNexusTourShow (tour.go) with `nexus tour`, same drift-proofing
// reason as handleNexusExplainerTool. "No tour with this slug" is an
// ordinary result, not a tool error — only actually failing to resolve a
// repo or read a blob is treated as an error here.
func handleNexusTourTool(ctx context.Context, slug string) (any, *mcpError) {
	if slug == "" {
		return mcpToolErrorResult("invalid params: slug is required"), nil
	}

	repoRoot, err := paths.WorktreeRoot(ctx)
	if err != nil {
		return mcpToolErrorResult("not a git repository"), nil
	}

	result, err := computeNexusTourShow(ctx, repoRoot, slug)
	if err != nil {
		return mcpToolErrorResult(err.Error()), nil
	}

	data, err := json.Marshal(result)
	if err != nil {
		return mcpToolErrorResult(fmt.Sprintf("encode result: %v", err)), nil
	}
	return mcpToolTextResult(string(data)), nil
}

// handleNexusSpeakTool serves the nexus_speak MCP tool. It shares
// runNexusSpeak (speak.go) with `nexus speak`, always detached so a long
// narrative returns as soon as audio starts instead of blocking the agent.
// "Nothing to read" outcomes (no entry, no summary, Nexus not set up) are
// ordinary results carrying an 'error' field; only "no speech engine on
// this machine" or a failure to spawn it is a tool error.
func handleNexusSpeakTool(ctx context.Context, req nexusSpeakRequest) (any, *mcpError) {
	if !req.Stop && req.Path == "" && req.Text == "" {
		return mcpToolErrorResult("invalid params: path or text is required (or stop=true)"), nil
	}

	result, err := runNexusSpeak(ctx, req)
	if err != nil {
		return mcpToolErrorResult(err.Error()), nil
	}

	data, err := json.Marshal(result)
	if err != nil {
		return mcpToolErrorResult(fmt.Sprintf("encode result: %v", err)), nil
	}
	return mcpToolTextResult(string(data)), nil
}

func mcpToolTextResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

func mcpToolErrorResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": true,
	}
}
