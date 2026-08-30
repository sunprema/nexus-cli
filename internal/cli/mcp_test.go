package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type mcpTestResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// driveMCP runs the server against a sequence of newline-delimited requests and
// returns the decoded responses (notifications produce none).
func driveMCP(t *testing.T, requests ...string) []mcpTestResp {
	t.Helper()
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	var out bytes.Buffer
	if err := runMCPServer(context.Background(), in, &out); err != nil {
		t.Fatalf("runMCPServer: %v", err)
	}
	var resps []mcpTestResp
	dec := json.NewDecoder(&out)
	for dec.More() {
		var r mcpTestResp
		if err := dec.Decode(&r); err != nil {
			t.Fatalf("decode response: %v (raw: %s)", err, out.String())
		}
		resps = append(resps, r)
	}
	return resps
}

func mcpResultText(t *testing.T, result json.RawMessage) string {
	t.Helper()
	var res struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(result, &res); err != nil {
		t.Fatalf("unmarshal tool result content: %v", err)
	}
	if len(res.Content) == 0 {
		t.Fatalf("tool result had no content: %s", result)
	}
	return res.Content[0].Text
}

// The MCP handshake: initialize echoes the client's protocolVersion and returns
// serverInfo + the tools capability; the `notifications/initialized` notification
// gets no response.
func TestMCPServer_InitializeHandshake(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
	)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response (the notification is silent), got %d", len(resps))
	}
	var res struct {
		ProtocolVersion string                     `json:"protocolVersion"`
		Capabilities    map[string]json.RawMessage `json:"capabilities"`
		ServerInfo      struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(resps[0].Result, &res); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}
	if res.ServerInfo.Name != mcpServerName {
		t.Errorf("serverInfo.name = %q, want %q", res.ServerInfo.Name, mcpServerName)
	}
	if res.ProtocolVersion != "2024-11-05" {
		t.Errorf("initialize should echo the client's protocolVersion, got %q", res.ProtocolVersion)
	}
	if _, ok := res.Capabilities["tools"]; !ok {
		t.Errorf("initialize result should advertise the tools capability, got %v", res.Capabilities)
	}
}

// When the client omits protocolVersion, the server advertises its own default.
func TestMCPServer_InitializeDefaultProtocol(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(resps[0].Result, &res); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}
	if res.ProtocolVersion != mcpProtocolVersion {
		t.Errorf("default protocolVersion = %q, want %q", res.ProtocolVersion, mcpProtocolVersion)
	}
}

// A non-string protocolVersion is ignored gracefully; the server falls back to
// its default instead of erroring.
func TestMCPServer_InitializeBadProtocolType(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":123}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	var res struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(resps[0].Result, &res); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}
	if res.ProtocolVersion != mcpProtocolVersion {
		t.Errorf("bad protocolVersion type should fall back to default %q, got %q", mcpProtocolVersion, res.ProtocolVersion)
	}
}

func TestMCPServer_Ping(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":8,"method":"ping"}`)
	if len(resps) != 1 || resps[0].Error != nil {
		t.Fatalf("ping should return exactly one non-error response, got %+v", resps)
	}
}

func TestMCPServer_ToolsList(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	var res struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resps[0].Result, &res); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	names := map[string]bool{}
	for _, tl := range res.Tools {
		names[tl.Name] = true
	}
	for _, want := range []string{"nexus_explainer", "nexus_map", "nexus_tour"} {
		if !names[want] {
			t.Errorf("tools/list missing %q; got %v", want, names)
		}
	}
}

// This repo has Nexus set up on itself (dogfooding), so this exercises the
// tool against real repo state rather than a fixture — it asserts shape,
// not specific found/desynced values, since those can legitimately change
// as this repo's own explainer branch evolves.
func TestMCPServer_NexusExplainerToolCall(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"nexus_explainer","arguments":{"path":"internal/cli/show.go"}}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	text := mcpResultText(t, resps[0].Result)
	var result struct {
		Path            string `json:"path"`
		ExplainerPath   string `json:"explainer_path"`
		ExplainerBranch string `json:"explainer_branch"`
		Found           bool   `json:"found"`
		Desynced        bool   `json:"desynced"`
		Content         string `json:"content"`
		Error           string `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("nexus_explainer tool should return nexusShowResult JSON, got %q (err %v)", text, err)
	}
	if result.Path != "internal/cli/show.go" {
		t.Errorf("expected path echoed back as given, got %q", result.Path)
	}
	if result.ExplainerBranch == "" && result.Error == "" {
		t.Errorf("expected either explainer_branch resolved or an error explaining why not, got %+v", result)
	}
}

func TestMCPServer_NexusExplainerToolCall_MissingPathIsToolError(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"nexus_explainer","arguments":{}}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	var res struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resps[0].Result, &res); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if !res.IsError {
		t.Fatalf("missing path argument should be a tool-level error, got %s", resps[0].Result)
	}
}

// This repo has Nexus set up on itself (dogfooding), so this exercises the
// tool against real repo state — like TestMCPServer_NexusExplainerToolCall,
// it asserts shape, not a specific count, since that grows as this repo's
// own explainer branch does.
func TestMCPServer_NexusMapToolCall(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":14,"method":"tools/call","params":{"name":"nexus_map","arguments":{}}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	text := mcpResultText(t, resps[0].Result)
	var result struct {
		ExplainerBranch string `json:"explainer_branch"`
		Count           int    `json:"count"`
		WithSummary     int    `json:"with_summary"`
		Entries         []struct {
			Path           string `json:"path"`
			Summary        string `json:"summary"`
			Desynced       bool   `json:"desynced"`
			HasFrontmatter bool   `json:"has_frontmatter"`
		} `json:"entries"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("nexus_map tool should return nexusMapResult JSON, got %q (err %v)", text, err)
	}
	if result.ExplainerBranch == "" && result.Error == "" {
		t.Errorf("expected either explainer_branch resolved or an error explaining why not, got %+v", result)
	}
	if len(result.Entries) != result.Count {
		t.Errorf("count %d should match len(entries) %d", result.Count, len(result.Entries))
	}
	if result.WithSummary > result.Count {
		t.Errorf("with_summary %d cannot exceed count %d", result.WithSummary, result.Count)
	}
}

// The tour slug won't exist in this repo (no guided tours have been written
// yet), so this exercises the ordinary "not found" result rather than a
// protocol- or tool-level error.
func TestMCPServer_NexusTourToolCall_NotFound(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":15,"method":"tools/call","params":{"name":"nexus_tour","arguments":{"slug":"does-not-exist"}}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	text := mcpResultText(t, resps[0].Result)
	var result struct {
		Slug            string `json:"slug"`
		ExplainerBranch string `json:"explainer_branch"`
		Found           bool   `json:"found"`
		Error           string `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("nexus_tour tool should return nexusTourResult JSON, got %q (err %v)", text, err)
	}
	if result.Found {
		t.Errorf("slug %q should not exist in this repo yet, got found=true", result.Slug)
	}
}

func TestMCPServer_NexusTourToolCall_MissingSlugIsToolError(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":16,"method":"tools/call","params":{"name":"nexus_tour","arguments":{}}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	var res struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resps[0].Result, &res); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if !res.IsError {
		t.Fatalf("missing slug argument should be a tool-level error, got %s", resps[0].Result)
	}
}

func TestMCPServer_EmptyToolName(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":""}}`)
	if len(resps) != 1 || resps[0].Error == nil || resps[0].Error.Code != -32602 {
		t.Fatalf("empty tool name should return invalid-params (-32602), got %+v", resps)
	}
}

func TestMCPServer_UnknownToolIsProtocolError(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"no-such-tool"}}`)
	if len(resps) != 1 || resps[0].Error == nil {
		t.Fatalf("expected one error response for an unknown tool, got %+v", resps)
	}
}

func TestMCPServer_UnknownMethodReturnsError(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":6,"method":"bogus/method"}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	if resps[0].Error == nil || resps[0].Error.Code != -32601 {
		t.Errorf("expected method-not-found (-32601), got: %+v", resps[0].Error)
	}
}

// A parseable-but-invalid request (missing method or wrong jsonrpc version) is
// rejected with -32600 before dispatch, not treated as method-not-found.
func TestMCPServer_InvalidRequest(t *testing.T) {
	t.Parallel()
	for _, req := range []string{
		`{"jsonrpc":"2.0","id":1}`,                 // missing method
		`{"jsonrpc":"1.0","id":2,"method":"ping"}`, // wrong jsonrpc version
	} {
		resps := driveMCP(t, req)
		if len(resps) != 1 || resps[0].Error == nil || resps[0].Error.Code != -32600 {
			t.Errorf("request %s should be rejected with -32600 (invalid request), got %+v", req, resps)
		}
	}
}

// runMCPServer must echo the request id verbatim (numeric or string); a
// dropped/swapped id silently breaks multi-call MCP sessions.
func TestMCPServer_EchoesRequestID(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t,
		`{"jsonrpc":"2.0","id":42,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":"abc","method":"ping"}`,
	)
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses, got %d", len(resps))
	}
	if string(resps[0].ID) != "42" {
		t.Errorf("numeric id should round-trip verbatim, got %s", resps[0].ID)
	}
	if string(resps[1].ID) != `"abc"` {
		t.Errorf("string id should round-trip verbatim, got %s", resps[1].ID)
	}
}

// A single line larger than maxMCPMessageBytes is rejected without consuming
// unbounded memory, and the server stops cleanly.
func TestMCPServer_OversizedMessageRejected(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("x", maxMCPMessageBytes+1024)
	line := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nexus_explainer","arguments":{"path":"` + big + `"}}}` + "\n"
	var out bytes.Buffer
	if err := runMCPServer(context.Background(), strings.NewReader(line), &out); err != nil {
		t.Fatalf("runMCPServer: %v", err)
	}
	var resp mcpTestResp
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("expected an oversize error response, got %q (err %v)", out.String(), err)
	}
	if resp.Error == nil || resp.Error.Code != -32600 {
		t.Errorf("expected request-too-large (-32600), got %+v", resp.Error)
	}
}

// A JSON-RPC batch array is unsupported: it yields a parse error, and the server
// recovers to the next line instead of terminating.
func TestMCPServer_BatchArrayRejectedThenRecovers(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t,
		`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	)
	if len(resps) != 2 {
		t.Fatalf("expected 2 responses (parse error, then the recovered ping), got %d: %+v", len(resps), resps)
	}
	if resps[0].Error == nil || resps[0].Error.Code != -32700 {
		t.Errorf("batch array should yield a parse error (-32700), got %+v", resps[0].Error)
	}
	if resps[1].Error != nil {
		t.Errorf("server should recover and serve the next message, got error %+v", resps[1].Error)
	}
}

// Malformed JSON in the stream is reported as a JSON-RPC parse error (-32700).
func TestMCPServer_ParseError(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := runMCPServer(context.Background(), strings.NewReader("{not valid json\n"), &out); err != nil {
		t.Fatalf("runMCPServer: %v", err)
	}
	var resp mcpTestResp
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("expected a JSON parse-error response, got %q (err %v)", out.String(), err)
	}
	if resp.Error == nil || resp.Error.Code != -32700 {
		t.Errorf("expected parse error -32700, got %+v", resp.Error)
	}
}

// nexus_speak is a side-effecting tool, so its tests never let it reach a
// real engine: print=true returns the prepared text without one, and
// argument validation is checked as tool-level (not protocol-level) errors.
func TestMCPServer_ToolsListIncludesSpeak(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":40,"method":"tools/list"}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	var res struct {
		Tools []struct {
			Name        string `json:"name"`
			InputSchema struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(resps[0].Result, &res); err != nil {
		t.Fatalf("unmarshal tools/list: %v", err)
	}
	for _, tool := range res.Tools {
		if tool.Name != "nexus_speak" {
			continue
		}
		for _, prop := range []string{"path", "mode", "text", "voice", "stop", "print"} {
			if _, ok := tool.InputSchema.Properties[prop]; !ok {
				t.Errorf("nexus_speak schema missing property %q", prop)
			}
		}
		return
	}
	t.Fatalf("nexus_speak not advertised in tools/list: %s", resps[0].Result)
}

func TestMCPServer_NexusSpeakToolCall_PrintReturnsText(t *testing.T) {
	withNoTTSEngine(t)
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":41,"method":"tools/call","params":{"name":"nexus_speak","arguments":{"text":"Read **this** aloud.","print":true}}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	text := mcpResultText(t, resps[0].Result)
	var result struct {
		Spoke            bool   `json:"spoke"`
		Words            int    `json:"words"`
		EstimatedSeconds int    `json:"estimated_seconds"`
		Text             string `json:"text"`
		Error            string `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		t.Fatalf("nexus_speak should return nexusSpeakResult JSON, got %q (err %v)", text, err)
	}
	if result.Spoke || result.Text != "Read this aloud." || result.Words != 3 || result.Error != "" {
		t.Errorf("unexpected print result %+v", result)
	}
}

func TestMCPServer_NexusSpeakToolCall_MissingArgsIsToolError(t *testing.T) {
	t.Parallel()
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"nexus_speak","arguments":{}}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	var res struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resps[0].Result, &res); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if !res.IsError {
		t.Fatalf("neither path nor text should be a tool-level error, got %s", resps[0].Result)
	}
}

func TestMCPServer_NexusSpeakToolCall_NoEngineIsToolError(t *testing.T) {
	withNoTTSEngine(t)
	resps := driveMCP(t, `{"jsonrpc":"2.0","id":43,"method":"tools/call","params":{"name":"nexus_speak","arguments":{"text":"hello"}}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	var res struct {
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(resps[0].Result, &res); err != nil {
		t.Fatalf("unmarshal tool result: %v", err)
	}
	if !res.IsError || !strings.Contains(mcpResultText(t, resps[0].Result), "no text-to-speech engine") {
		t.Fatalf("missing engine should be a tool-level error naming the problem, got %s", resps[0].Result)
	}
}
