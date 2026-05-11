package a2aserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2aclient"
	"github.com/a2aproject/a2a-go/v2/a2aclient/agentcard"
	"github.com/a2aproject/a2a-go/v2/a2acompat/a2av0"

	"github.com/seanly/dmr-devkit/agent"
)

type stubRunner struct {
	out string
	err error
}

func (s *stubRunner) Run(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32) (*agent.Result, error) {
	if s.err != nil {
		return nil, s.err
	}
	_ = tapeName
	return &agent.RunResult{Output: s.out + ":" + prompt}, nil
}

type recordingRunner struct {
	mu    sync.Mutex
	tapes []string
	out   string
}

func (r *recordingRunner) Run(ctx context.Context, tapeName, prompt string, historyAfterEntryID int32) (*agent.Result, error) {
	r.mu.Lock()
	r.tapes = append(r.tapes, tapeName)
	r.mu.Unlock()
	return &agent.RunResult{Output: r.out + ":" + prompt}, nil
}

func TestMount_sendMessage_roundTrip(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	err := Mount(mux, Options{
		AgentName:       "test-agent",
		Description:     "test",
		PublicInvokeURL: srv.URL + "/invoke",
		MountPath:       "/invoke",
		TapeMode:        TapeModeFixed,
		TapeName:        "t1",
	}, &stubRunner{out: "ok"})
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	card, err := agentcard.NewResolver(srv.Client()).Resolve(ctx, srv.URL)
	if err != nil {
		t.Fatalf("fetch card: %v", err)
	}
	cli, err := a2aclient.NewFromCard(ctx, card, a2av0.WithJSONRPCTransport(a2av0.JSONRPCTransportConfig{Client: srv.Client()}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Destroy() })

	msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("ping"))
	res, err := cli.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := res.(*a2a.Message)
	if !ok || m == nil {
		t.Fatalf("result type %T", res)
	}
	if got := messageUserText(m); got != "ok:ping" {
		t.Fatalf("got %q", got)
	}
}

func TestMount_autoTape_distinctPerNewTask(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	rec := &recordingRunner{out: "ok"}
	err := Mount(mux, Options{
		AgentName:       "test-agent",
		Description:     "test",
		PublicInvokeURL: srv.URL + "/invoke",
		MountPath:       "/invoke",
		TapeMode:        TapeModeAuto,
		TapePrefix:      "a2a",
	}, rec)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	card, err := agentcard.NewResolver(srv.Client()).Resolve(ctx, srv.URL)
	if err != nil {
		t.Fatalf("fetch card: %v", err)
	}
	cli, err := a2aclient.NewFromCard(ctx, card, a2av0.WithJSONRPCTransport(a2av0.JSONRPCTransportConfig{Client: srv.Client()}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Destroy() })

	for _, p := range []string{"a", "b"} {
		msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart(p))
		_, err := cli.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
		if err != nil {
			t.Fatal(err)
		}
	}
	rec.mu.Lock()
	tapes := append([]string(nil), rec.tapes...)
	rec.mu.Unlock()
	if len(tapes) != 2 {
		t.Fatalf("tapes=%v", tapes)
	}
	if tapes[0] == tapes[1] {
		t.Fatalf("expected two distinct tapes, got %q twice", tapes[0])
	}
	if !strings.HasPrefix(tapes[0], "a2a_") || !strings.HasPrefix(tapes[1], "a2a_") {
		t.Fatalf("expected a2a_ prefix: %v", tapes)
	}
}

func TestMount_fixedTape_sameTapeTwice(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	rec := &recordingRunner{out: "ok"}
	err := Mount(mux, Options{
		AgentName:       "test-agent",
		Description:     "test",
		PublicInvokeURL: srv.URL + "/invoke",
		MountPath:       "/invoke",
		TapeMode:        TapeModeFixed,
		TapeName:        "shared",
	}, rec)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	card, err := agentcard.NewResolver(srv.Client()).Resolve(ctx, srv.URL)
	if err != nil {
		t.Fatalf("fetch card: %v", err)
	}
	cli, err := a2aclient.NewFromCard(ctx, card, a2av0.WithJSONRPCTransport(a2av0.JSONRPCTransportConfig{Client: srv.Client()}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Destroy() })

	for range 2 {
		msg := a2a.NewMessage(a2a.MessageRoleUser, a2a.NewTextPart("x"))
		_, err := cli.SendMessage(ctx, &a2a.SendMessageRequest{Message: msg})
		if err != nil {
			t.Fatal(err)
		}
	}
	rec.mu.Lock()
	tapes := append([]string(nil), rec.tapes...)
	rec.mu.Unlock()
	if tapes[0] != "shared" || tapes[1] != "shared" {
		t.Fatalf("got %v", tapes)
	}
}

func TestMount_invalidTapeMode(t *testing.T) {
	t.Parallel()
	err := Mount(http.NewServeMux(), Options{
		PublicInvokeURL: "http://x/invoke",
		TapeMode:        TapeMode("bogus"),
	}, &stubRunner{})
	if err == nil {
		t.Fatal("expected error")
	}
}
