package tape

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"time"
)

// ExportFormat is the output serialization for tape export.
type ExportFormat string

const (
	ExportFormatMarkdown ExportFormat = "markdown"
	ExportFormatJSON     ExportFormat = "json"
	ExportFormatHTML     ExportFormat = "html"
)

// ExportMode selects transcript (all kinds) vs chat-oriented view.
type ExportMode string

const (
	ExportModeTranscript ExportMode = "transcript"
	ExportModeChat       ExportMode = "chat"
)

// ExportMeta describes the exported slice for all formats.
type ExportMeta struct {
	TapeName         string
	SliceDescription string
	GeneratedAt      string
	EntryCount       int
}

// ExportOptions selects format and mode.
type ExportOptions struct {
	Format ExportFormat
	Mode   ExportMode
}

// ChatExportMessage is one chat-oriented row (merged tools, like web history).
type ChatExportMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ChatExportTool `json:"tool_calls,omitempty"`
}

// ChatExportTool is a tool invocation with optional result (chat / JSON export).
type ChatExportTool struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
	Result    string `json:"result,omitempty"`
}

const exportJSONVersion = 1

// ExportTape serializes tape entries according to meta, format, and mode.
func ExportTape(meta ExportMeta, entries []TapeEntry, opt ExportOptions) ([]byte, error) {
	if meta.GeneratedAt == "" {
		meta.GeneratedAt = time.Now().Format(time.RFC3339)
	}
	meta.EntryCount = len(entries)

	switch opt.Format {
	case ExportFormatMarkdown:
		return []byte(exportMarkdown(meta, entries, opt.Mode)), nil
	case ExportFormatJSON:
		return exportJSON(meta, entries, opt.Mode)
	case ExportFormatHTML:
		return []byte(exportHTML(meta, entries, opt.Mode)), nil
	default:
		return nil, fmt.Errorf("unknown export format: %q", opt.Format)
	}
}

// BuildChatExportMessages builds the merged user/assistant history with tool calls,
// plus system and compact_summary rows, in tape order (same idea as web tapeReaderAdapter).
func BuildChatExportMessages(entries []TapeEntry) []ChatExportMessage {
	var out []ChatExportMessage
	var pending []ChatExportTool

	flushPending := func() {
		if len(pending) == 0 {
			return
		}
		// Orphan tool calls: attach to an empty assistant stub so data is not lost.
		out = append(out, ChatExportMessage{Role: "assistant", Content: "", ToolCalls: pending})
		pending = nil
	}

	for _, e := range entries {
		switch e.Kind {
		case "system":
			flushPending()
			if content, ok := e.Payload["content"].(string); ok {
				out = append(out, ChatExportMessage{Role: "system", Content: content})
			}
		case "compact_summary":
			flushPending()
			if content, ok := e.Payload["content"].(string); ok {
				out = append(out, ChatExportMessage{Role: "system", Content: "[Context Summary]\n" + content})
			}
		case "message":
			role, _ := e.Payload["role"].(string)
			content, _ := e.Payload["content"].(string)
			if role == "" || (role != "user" && role != "assistant") {
				continue
			}
			if content == "" && len(pending) == 0 {
				continue
			}
			msg := ChatExportMessage{Role: role, Content: content}
			if len(pending) > 0 {
				msg.ToolCalls = pending
				pending = nil
			}
			out = append(out, msg)
		case "tool_call":
			calls, ok := ExtractToolCalls(e.Payload)
			if !ok {
				continue
			}
			for _, c := range calls {
				pending = append(pending, ChatExportTool{Name: c.Name, Arguments: c.Arguments})
			}
		case "tool_result":
			results, ok := ExtractToolResults(e.Payload)
			if !ok {
				continue
			}
			for i, r := range results {
				if i < len(pending) {
					pending[i].Result = r.Content
				}
			}
		}
	}
	flushPending()
	return out
}

func exportJSON(meta ExportMeta, entries []TapeEntry, mode ExportMode) ([]byte, error) {
	type metaObj struct {
		Tape        string `json:"tape"`
		Slice       string `json:"slice"`
		GeneratedAt string `json:"generated_at"`
		EntryCount  int    `json:"entry_count"`
	}

	doc := struct {
		Version int         `json:"version"`
		Meta    metaObj     `json:"meta"`
		Mode    ExportMode  `json:"mode"`
		Entries interface{} `json:"entries"`
	}{
		Version: exportJSONVersion,
		Meta: metaObj{
			Tape:        meta.TapeName,
			Slice:       meta.SliceDescription,
			GeneratedAt: meta.GeneratedAt,
			EntryCount:  meta.EntryCount,
		},
		Mode: mode,
	}

	switch mode {
	case ExportModeChat:
		doc.Entries = BuildChatExportMessages(entries)
	case ExportModeTranscript:
		doc.Entries = entries
	default:
		return nil, fmt.Errorf("unknown export mode: %q", mode)
	}

	return json.MarshalIndent(doc, "", "  ")
}

func exportMarkdown(meta ExportMeta, entries []TapeEntry, mode ExportMode) string {
	var b strings.Builder
	b.WriteString("# Tape export\n\n")
	b.WriteString(fmt.Sprintf("- **tape:** %s\n", meta.TapeName))
	b.WriteString(fmt.Sprintf("- **slice:** %s\n", meta.SliceDescription))
	b.WriteString(fmt.Sprintf("- **mode:** %s\n", mode))
	b.WriteString(fmt.Sprintf("- **entries:** %d\n", len(entries)))
	b.WriteString(fmt.Sprintf("- **generated_at:** %s\n\n", meta.GeneratedAt))

	switch mode {
	case ExportModeChat:
		for i, msg := range BuildChatExportMessages(entries) {
			b.WriteString(fmt.Sprintf("## Message %d — %s\n\n", i+1, msg.Role))
			if msg.Content != "" {
				b.WriteString(msg.Content)
				b.WriteString("\n\n")
			}
			for _, tc := range msg.ToolCalls {
				b.WriteString(fmt.Sprintf("### tool: `%s`\n\n", tc.Name))
				if tc.Arguments != "" {
					b.WriteString("Arguments:\n\n")
					b.WriteString("```json\n")
					b.WriteString(prettyJSONIfObject(tc.Arguments))
					b.WriteString("\n```\n\n")
				}
				if tc.Result != "" {
					b.WriteString("Result:\n\n")
					b.WriteString("```\n")
					b.WriteString(tc.Result)
					b.WriteString("\n```\n\n")
				}
			}
		}
	case ExportModeTranscript:
		for _, e := range entries {
			writeMarkdownEntry(&b, e)
		}
	default:
		b.WriteString(fmt.Sprintf("(unknown mode %q)\n", mode))
	}
	return b.String()
}

func writeMarkdownEntry(b *strings.Builder, e TapeEntry) {
	b.WriteString(fmt.Sprintf("## Entry %d — %s — %s\n\n", e.ID, e.Kind, e.Date))
	if len(e.Meta) > 0 {
		raw, _ := json.MarshalIndent(e.Meta, "", "  ")
		b.WriteString("**meta:**\n\n```json\n")
		b.WriteString(string(raw))
		b.WriteString("\n```\n\n")
	}

	switch e.Kind {
	case "message":
		role, _ := e.Payload["role"].(string)
		content, _ := e.Payload["content"].(string)
		b.WriteString(fmt.Sprintf("**role:** %s\n\n", role))
		b.WriteString(content)
		b.WriteString("\n\n")
	case "system", "compact_summary":
		if content, ok := e.Payload["content"].(string); ok {
			b.WriteString(content)
			b.WriteString("\n\n")
		}
	case "anchor":
		raw, _ := json.MarshalIndent(e.Payload, "", "  ")
		b.WriteString("```json\n")
		b.WriteString(string(raw))
		b.WriteString("\n```\n\n")
	case "tool_call":
		calls, ok := ExtractToolCalls(e.Payload)
		if !ok {
			raw, _ := json.MarshalIndent(e.Payload, "", "  ")
			b.WriteString("```json\n")
			b.WriteString(string(raw))
			b.WriteString("\n```\n\n")
			break
		}
		for _, c := range calls {
			b.WriteString(fmt.Sprintf("- **%s** (`%s`)\n\n", c.Name, c.ID))
			b.WriteString("```json\n")
			b.WriteString(prettyJSONIfObject(c.Arguments))
			b.WriteString("\n```\n\n")
		}
	case "tool_result":
		results, ok := ExtractToolResults(e.Payload)
		if !ok {
			raw, _ := json.MarshalIndent(e.Payload, "", "  ")
			b.WriteString("```json\n")
			b.WriteString(string(raw))
			b.WriteString("\n```\n\n")
			break
		}
		for _, r := range results {
			b.WriteString("```\n")
			b.WriteString(r.Content)
			b.WriteString("\n```\n\n")
		}
	default:
		raw, _ := json.MarshalIndent(e.Payload, "", "  ")
		b.WriteString("```json\n")
		b.WriteString(string(raw))
		b.WriteString("\n```\n\n")
	}
}

func htmlEntryKindClass(kind string) string {
	switch kind {
	case "message":
		return "k-message"
	case "system":
		return "k-system"
	case "compact_summary":
		return "k-compact"
	case "anchor":
		return "k-anchor"
	case "tool_call":
		return "k-toolcall"
	case "tool_result":
		return "k-toolresult"
	case "error":
		return "k-error"
	case "event":
		return "k-event"
	default:
		return "k-other"
	}
}

func htmlMessageRoleClass(role string) string {
	switch strings.TrimSpace(role) {
	case "user":
		return "r-user"
	case "assistant":
		return "r-assistant"
	case "system":
		return "r-system"
	default:
		return "r-other"
	}
}

func writeHTMLTranscriptEntry(body *strings.Builder, e TapeEntry) {
	secClass := "entry " + htmlEntryKindClass(e.Kind)
	if e.Kind == "message" {
		role, _ := e.Payload["role"].(string)
		secClass += " " + htmlMessageRoleClass(role)
	}
	body.WriteString(`<details class="foldable ` + secClass + `" open>`)
	body.WriteString(fmt.Sprintf(`<summary class="entry-title">Entry %d — %s — %s</summary>`, e.ID, html.EscapeString(e.Kind), html.EscapeString(e.Date)))
	if len(e.Meta) > 0 {
		raw, _ := json.MarshalIndent(e.Meta, "", "  ")
		body.WriteString(`<h3 class="subhdr subhdr-meta">meta</h3><pre><code>`)
		body.WriteString(html.EscapeString(string(raw)))
		body.WriteString(`</code></pre>`)
	}

	switch e.Kind {
	case "message":
		role, _ := e.Payload["role"].(string)
		content, _ := e.Payload["content"].(string)
		body.WriteString(fmt.Sprintf(`<p class="role"><strong>%s</strong></p>`, html.EscapeString(role)))
		body.WriteString(`<pre class="raw"><code>`)
		body.WriteString(html.EscapeString(content))
		body.WriteString(`</code></pre>`)
	case "system", "compact_summary":
		content, _ := e.Payload["content"].(string)
		body.WriteString(`<pre class="raw"><code>`)
		body.WriteString(html.EscapeString(content))
		body.WriteString(`</code></pre>`)
	case "anchor":
		raw, _ := json.MarshalIndent(e.Payload, "", "  ")
		body.WriteString(`<h3 class="subhdr subhdr-anchor">anchor</h3><pre><code>`)
		body.WriteString(html.EscapeString(string(raw)))
		body.WriteString(`</code></pre>`)
	case "tool_call":
		calls, ok := ExtractToolCalls(e.Payload)
		if !ok {
			raw, _ := json.MarshalIndent(e.Payload, "", "  ")
			body.WriteString(`<pre><code>`)
			body.WriteString(html.EscapeString(string(raw)))
			body.WriteString(`</code></pre>`)
			body.WriteString(`</details>`)
			return
		}
		for _, c := range calls {
			body.WriteString(fmt.Sprintf(`<h3 class="subhdr subhdr-toolcall">tool_call: %s</h3>`, html.EscapeString(c.Name)))
			if c.ID != "" {
				body.WriteString(fmt.Sprintf(`<p class="muted">id: %s</p>`, html.EscapeString(c.ID)))
			}
			body.WriteString(`<pre><code>`)
			body.WriteString(html.EscapeString(prettyJSONIfObject(c.Arguments)))
			body.WriteString(`</code></pre>`)
		}
	case "tool_result":
		results, ok := ExtractToolResults(e.Payload)
		if !ok {
			raw, _ := json.MarshalIndent(e.Payload, "", "  ")
			body.WriteString(`<pre><code>`)
			body.WriteString(html.EscapeString(string(raw)))
			body.WriteString(`</code></pre>`)
			body.WriteString(`</details>`)
			return
		}
		body.WriteString(`<h3 class="subhdr subhdr-toolresult">tool_result</h3>`)
		for _, r := range results {
			body.WriteString(`<pre class="raw"><code>`)
			body.WriteString(html.EscapeString(r.Content))
			body.WriteString(`</code></pre>`)
		}
	default:
		raw, _ := json.MarshalIndent(e.Payload, "", "  ")
		body.WriteString(`<pre><code>`)
		body.WriteString(html.EscapeString(string(raw)))
		body.WriteString(`</code></pre>`)
	}
	body.WriteString(`</details>`)
}

func prettyJSONIfObject(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || !json.Valid([]byte(s)) {
		return s
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(s), "", "  "); err != nil {
		return s
	}
	return buf.String()
}

func exportHTML(meta ExportMeta, entries []TapeEntry, mode ExportMode) string {
	var body strings.Builder
	body.WriteString(`<header class="meta"><h1>Tape export</h1><dl>`)
	body.WriteString(fmt.Sprintf(`<dt>tape</dt><dd>%s</dd>`, html.EscapeString(meta.TapeName)))
	body.WriteString(fmt.Sprintf(`<dt>slice</dt><dd>%s</dd>`, html.EscapeString(meta.SliceDescription)))
	body.WriteString(fmt.Sprintf(`<dt>mode</dt><dd>%s</dd>`, html.EscapeString(string(mode))))
	body.WriteString(fmt.Sprintf(`<dt>entries</dt><dd>%d</dd>`, len(entries)))
	body.WriteString(fmt.Sprintf(`<dt>generated_at</dt><dd>%s</dd>`, html.EscapeString(meta.GeneratedAt)))
	body.WriteString(`</dl></header>`)
	body.WriteString(`<p class="fold-controls"><button type="button" id="expand-all">Expand all</button> <button type="button" id="collapse-all">Collapse all</button></p>`)

	switch mode {
	case ExportModeChat:
		for i, msg := range BuildChatExportMessages(entries) {
			rc := htmlMessageRoleClass(msg.Role)
			body.WriteString(`<details class="foldable msg ` + rc + `" open>`)
			body.WriteString(fmt.Sprintf(`<summary class="msg-title">Message %d — %s</summary>`, i+1, html.EscapeString(msg.Role)))
			if msg.Content != "" {
				body.WriteString(`<pre class="raw"><code>`)
				body.WriteString(html.EscapeString(msg.Content))
				body.WriteString(`</code></pre>`)
			}
			for _, tc := range msg.ToolCalls {
				body.WriteString(`<div class="tool">`)
				body.WriteString(fmt.Sprintf(`<h3 class="tool-title">tool: %s</h3>`, html.EscapeString(tc.Name)))
				if tc.Arguments != "" {
					body.WriteString(`<p>Arguments</p><pre><code>`)
					body.WriteString(html.EscapeString(prettyJSONIfObject(tc.Arguments)))
					body.WriteString(`</code></pre>`)
				}
				if tc.Result != "" {
					body.WriteString(`<p>Result</p><pre class="raw"><code>`)
					body.WriteString(html.EscapeString(tc.Result))
					body.WriteString(`</code></pre>`)
				}
				body.WriteString(`</div>`)
			}
			body.WriteString(`</details>`)
		}
	case ExportModeTranscript:
		for _, e := range entries {
			writeHTMLTranscriptEntry(&body, e)
		}
	}

	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + html.EscapeString(meta.TapeName) + ` — tape export</title>
<style>
body{font-family:system-ui,sans-serif;line-height:1.45;max-width:900px;margin:1rem auto;padding:0 1rem;background:#1a1a1e;color:#e8e6e3}
header.meta dl{display:grid;grid-template-columns:8rem 1fr;gap:0.25rem 1rem}
header.meta dt{font-weight:600;color:#9ad}
.fold-controls{margin:0.5rem 0 1rem}
.fold-controls button{cursor:pointer;background:#333;color:#e8e6e3;border:1px solid #555;border-radius:4px;padding:0.35rem 0.65rem;font:inherit}
.fold-controls button:hover{background:#444}
details.foldable.entry,details.foldable.msg{border:1px solid #444;border-radius:6px;padding:0.5rem 0.75rem 0.75rem;margin:1rem 0;background:#222;border-left-width:4px;border-left-style:solid}
details.foldable > summary{cursor:pointer;list-style:none}
details.foldable > summary::-webkit-details-marker{display:none}
details.foldable > summary::before{content:'▶ ';font-size:0.75em;opacity:0.85;display:inline-block;width:1.1em}
details.foldable[open] > summary::before{content:'▼ '}
h1,h3{margin:0.4rem 0}
summary.entry-title,summary.msg-title{font-size:0.95rem;font-weight:600;margin:0 0 0.5rem;padding:0.4rem 0.55rem;border-radius:4px;display:block}
h3.subhdr{font-size:0.88rem;margin:0.6rem 0 0.35rem;padding:0.25rem 0.45rem;border-radius:3px}
h3.subhdr-meta{background:#2a2535;color:#c4b5fd}
h3.subhdr-anchor{background:#3d2e1f;color:#f0c674}
h3.subhdr-toolcall{background:#2d2540;color:#d4a5ff}
h3.subhdr-toolresult{background:#1f3540;color:#7fdbff}
h3.tool-title{background:#2a3038;color:#9ccfd8;font-size:0.88rem;padding:0.3rem 0.45rem;border-radius:3px}
p.role{margin:0.35rem 0}
p.muted{margin:0.25rem 0;font-size:0.85rem;color:#aaa}
pre,code{white-space:pre-wrap;word-break:break-word;font-family:ui-monospace,monospace;font-size:0.88rem}
pre{background:#111;padding:0.6rem;border-radius:4px;overflow:auto}
.tool{margin-top:0.75rem;padding-top:0.75rem;border-top:1px dashed #555}
pre.raw{margin:0.5rem 0}
/* Transcript: kind + role accent */
details.foldable.entry{border-left-color:#6b6b6b}
details.foldable.entry.k-message.r-user{border-left-color:#3d7dcc}
details.foldable.entry.k-message.r-assistant{border-left-color:#3d9e5c}
details.foldable.entry.k-message.r-system,details.foldable.entry.k-message.r-other{border-left-color:#9e9e9e}
details.foldable.entry.k-system,details.foldable.entry.k-compact{border-left-color:#c9a227}
details.foldable.entry.k-anchor{border-left-color:#d4943c}
details.foldable.entry.k-toolcall{border-left-color:#9b6bd6}
details.foldable.entry.k-toolresult{border-left-color:#3aa3c2}
details.foldable.entry.k-error{border-left-color:#d9534f}
details.foldable.entry.k-event{border-left-color:#6c7cff}
details.foldable.entry.k-other{border-left-color:#888}
details.foldable.entry.k-message.r-user summary.entry-title{background:#1a2f4d;color:#9dc7ff}
details.foldable.entry.k-message.r-assistant summary.entry-title{background:#1a3d2a;color:#9ee5b0}
details.foldable.entry.k-message.r-system summary.entry-title,details.foldable.entry.k-message.r-other summary.entry-title{background:#333;color:#ddd}
details.foldable.entry.k-system summary.entry-title,details.foldable.entry.k-compact summary.entry-title{background:#3d3518;color:#f2d98c}
details.foldable.entry.k-anchor summary.entry-title{background:#3d2e1f;color:#f0c674}
details.foldable.entry.k-toolcall summary.entry-title{background:#2d2540;color:#d4a5ff}
details.foldable.entry.k-toolresult summary.entry-title{background:#1f3540;color:#7fdbff}
details.foldable.entry.k-error summary.entry-title{background:#3d1f1f;color:#ff9a9a}
details.foldable.entry.k-event summary.entry-title{background:#252a45;color:#aab8ff}
details.foldable.entry.k-other summary.entry-title{background:#363636;color:#ccc}
/* Chat: role */
details.foldable.msg{border-left-color:#6b6b6b}
details.foldable.msg.r-user{border-left-color:#3d7dcc}
details.foldable.msg.r-assistant{border-left-color:#3d9e5c}
details.foldable.msg.r-system{border-left-color:#c9a227}
details.foldable.msg.r-other{border-left-color:#9e9e9e}
details.foldable.msg.r-user summary.msg-title{background:#1a2f4d;color:#9dc7ff}
details.foldable.msg.r-assistant summary.msg-title{background:#1a3d2a;color:#9ee5b0}
details.foldable.msg.r-system summary.msg-title{background:#3d3518;color:#f2d98c}
details.foldable.msg.r-other summary.msg-title{background:#333;color:#ddd}
</style>
</head>
<body>
` + body.String() + `
<script>
(function(){
function all(){return document.querySelectorAll('details.foldable');}
var ex=document.getElementById('expand-all');
var cl=document.getElementById('collapse-all');
if(ex)ex.onclick=function(){all().forEach(function(d){d.open=true;});};
if(cl)cl.onclick=function(){all().forEach(function(d){d.open=false;});};
})();
</script>
</body>
</html>`
}
