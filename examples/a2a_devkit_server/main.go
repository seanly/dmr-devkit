// Example: HTTP server that exposes a devkit-built [agent.Agent] as an A2A JSON-RPC endpoint.
//
// Another DMR instance can reach it with the a2a plugin configured with this server's base URL.
//
// Run:
//
//	AI_API_KEY=... AI_MODEL=... go run ./examples/a2a_devkit_server
//
// Tape naming (see pkg/a2aserver.Options): by default each A2A Task uses an isolated tape
// a2a_<taskId>. For the legacy single shared tape (not recommended under concurrent clients),
// set A2A_TAPE_MODE=fixed and optionally A2A_TAPE_NAME (default "default").
package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/seanly/dmr-devkit/a2aserver"
	"github.com/seanly/dmr-devkit/devkit"
)

func main() {
	ctx := context.Background()

	opts := devkit.EnvOptions()
	if opts.APIKey == "" || opts.Model == "" {
		log.Fatal("AI_API_KEY and AI_MODEL are required")
	}
	opts.Verbose = 1
	opts.SystemPromptExtra = "You are reachable via the A2A protocol. Keep replies concise."

	kit, err := devkit.Build(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = kit.Close(ctx) }()

	addr := ":8080"
	publicURL := os.Getenv("A2A_PUBLIC_INVOKE_URL")
	if publicURL == "" {
		publicURL = "http://127.0.0.1" + addr + "/invoke"
	}

	mountOpts := a2aserver.Options{
		AgentName:       "dmr-devkit",
		Description:     "DMR agent exposed via pkg/a2aserver",
		PublicInvokeURL: publicURL,
		MountPath:       "/invoke",
	}
	switch os.Getenv("A2A_TAPE_MODE") {
	case "fixed":
		mountOpts.TapeMode = a2aserver.TapeModeFixed
		mountOpts.TapeName = os.Getenv("A2A_TAPE_NAME")
		if mountOpts.TapeName == "" {
			mountOpts.TapeName = devkit.DefaultTapeName
		}
	default:
		if p := os.Getenv("A2A_TAPE_PREFIX"); p != "" {
			mountOpts.TapePrefix = p
		}
	}

	mux := http.NewServeMux()
	if err := a2aserver.Mount(mux, mountOpts, kit.Agent); err != nil {
		log.Fatal(err)
	}

	log.Printf("A2A listen %s — invoke URL %q — agent card http://127.0.0.1%s%s", addr, publicURL, addr, a2aserver.WellKnownAgentCardPath)
	if os.Getenv("A2A_PUBLIC_INVOKE_URL") == "" {
		log.Printf("Set A2A_PUBLIC_INVOKE_URL to the public invoke URL if clients use another host/port")
	}
	log.Fatal(http.ListenAndServe(addr, mux))
}
