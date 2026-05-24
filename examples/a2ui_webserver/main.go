// Example: HTTP server that exposes an agent via SSE streaming with A2UI support.
//
// Endpoints:
//   POST /chat         - send a prompt (JSON body: {"prompt":"..."})
//   GET  /a2ui/events  - SSE stream of A2UI widget events
//
// Run:
//
//	AI_API_KEY=... AI_MODEL=... go run ./examples/a2ui_webserver
//
// Then open the static HTML page in a browser:
//
//	http://localhost:8080/
package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/seanly/dmr-devkit/a2ui"
	"github.com/seanly/dmr-devkit/devkit"
	"github.com/seanly/dmr-devkit/webserver"
)

func main() {
	ctx := context.Background()

	opts := devkit.EnvOptions()
	if opts.APIKey == "" || opts.Model == "" {
		log.Fatal("AI_API_KEY and AI_MODEL are required")
	}
	opts.Verbose = 1

	// Inject the A2UI tool so the agent can emit UI widgets.
	opts.Tools = append(opts.Tools, a2ui.Tool())
	opts.SystemPromptExtra = a2ui.ExampleSystemPrompt()

	kit, err := devkit.Build(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = kit.Close(ctx) }()

	addr := ":8080"
	mux := http.NewServeMux()

	// Static demo page
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(demoHTML))
	})

	// POST /chat triggers the agent and returns the final text response.
	mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Prompt string `json:"prompt"` }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		res, err := kit.Agent.Run(r.Context(), devkit.DefaultTapeName, req.Prompt, 0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"output": res.Output})
	})

	// GET /a2ui/events streams workflow events (including A2UI widgets) over SSE.
	mux.HandleFunc("/a2ui/events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "GET only", http.StatusMethodNotAllowed)
			return
		}

		prompt := r.URL.Query().Get("prompt")
		if prompt == "" {
			prompt = "Hello"
		}

		webserver.SSEHeaders(w)

		node := kit.AsAgentNode("chat")
		// Stream only UI widget events for the frontend renderer.
		filtered := webserver.UIWidgetOnlyStream(node, 50*time.Millisecond)
		_ = webserver.StreamWorkflowEvents(r.Context(), w, filtered, prompt)
	})

	log.Printf("A2UI webserver listening on %s", addr)
	log.Printf("Open http://localhost%s/ for the demo page", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

const demoHTML = `<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>DMR A2UI Demo</title>
	<style>
		body { font-family: sans-serif; max-width: 800px; margin: 2rem auto; }
		#events { border: 1px solid #ccc; height: 300px; overflow-y: auto; padding: 1rem; background: #f9f9f9; }
		.event { margin-bottom: 0.5rem; font-family: monospace; font-size: 12px; }
		.event.ui { color: #0066cc; }
	</style>
</head>
<body>
	<h1>DMR A2UI SSE Demo</h1>
	<p>Type a prompt and watch A2UI widget events stream below.</p>
	<input id="prompt" type="text" value="Show me a simple greeting card" size="60">
	<button onclick="startStream()">Stream Events</button>
	<div id="events"></div>
	<script>
		function startStream() {
			const prompt = document.getElementById('prompt').value;
			const events = document.getElementById('events');
			events.innerHTML = '';
			const es = new EventSource('/a2ui/events?prompt=' + encodeURIComponent(prompt));
			es.addEventListener('ui_widget', function(e) {
				const div = document.createElement('div');
				div.className = 'event ui';
				const data = JSON.parse(e.data);
				div.textContent = '[A2UI] ' + JSON.stringify(data.ui_widget, null, 2);
				events.appendChild(div);
			});
			es.addEventListener('stream_error', function(e) {
				const div = document.createElement('div');
				div.className = 'event';
				div.style.color = 'red';
				div.textContent = '[ERROR] ' + e.data;
				events.appendChild(div);
				es.close();
			});
			es.addEventListener('workflow_end', function(e) {
				es.close();
			});
			es.onerror = function(e) {
				// Ignore native EventSource errors (no data) — they fire on normal disconnect.
				if (!e.data) return;
				const div = document.createElement('div');
				div.className = 'event';
				div.style.color = 'red';
				div.textContent = '[ERROR] ' + e.data;
				events.appendChild(div);
				es.close();
			};
		}
	</script>
</body>
</html>`
