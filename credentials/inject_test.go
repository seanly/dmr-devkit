package credentials

import (
	"context"
	"testing"
)

func TestResolveBindingsRemoteSecretText(t *testing.T) {
	st := newMemStore(t)
	ctx := context.Background()
	if err := st.Put(ctx, Credential{ID: "tok", Kind: KindSecretText, Data: []byte("secret-value")}); err != nil {
		t.Fatal(err)
	}
	env, files, err := ResolveBindingsRemote(ctx, st, []Binding{{CredentialID: "tok", Env: "API_KEY"}})
	if err != nil {
		t.Fatal(err)
	}
	if env["API_KEY"] != "secret-value" {
		t.Fatalf("env = %q", env["API_KEY"])
	}
	if len(files) != 0 {
		t.Fatalf("files = %v", files)
	}
}

func TestResolveBindingsRemoteSecretFile(t *testing.T) {
	st := newMemStore(t)
	ctx := context.Background()
	if err := st.Put(ctx, Credential{ID: "cfg", Kind: KindSecretFile, Data: []byte("kube-data")}); err != nil {
		t.Fatal(err)
	}
	_, files, err := ResolveBindingsRemote(ctx, st, []Binding{{CredentialID: "cfg", Env: "KUBECONFIG"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Env != "KUBECONFIG" {
		t.Fatalf("files = %+v", files)
	}
	if string(files[0].Content) != "kube-data" {
		t.Fatalf("content = %q", files[0].Content)
	}
}

func TestMaterializeBridgeInject(t *testing.T) {
	args := map[string]any{}
	if err := ApplyRemoteBindings(args, map[string]string{"X": "1"}, []RemoteFile{{Target: "ssh_key_path", Content: []byte("pem")}}); err != nil {
		t.Fatal(err)
	}
	cleanup, err := MaterializeBridgeInject(args)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	path, _ := args["ssh_key_path"].(string)
	if path == "" {
		t.Fatal("expected ssh_key_path")
	}
	if _, ok := args[RuntimeBridgeFilesKey]; ok {
		t.Fatal("bridge files key should be removed")
	}
}

func TestSanitizeWireArgs(t *testing.T) {
	args := map[string]any{
		"credential_bindings": []any{map[string]any{"credential_id": "x"}},
		"ssh_credential_id":   "key1",
		"cmd":                 "ls",
	}
	SanitizeWireArgs(args)
	if _, ok := args["credential_bindings"]; ok {
		t.Fatal("bindings should be stripped")
	}
	if _, ok := args["ssh_credential_id"]; ok {
		t.Fatal("ssh id should be stripped")
	}
	if args["cmd"] != "ls" {
		t.Fatal("cmd preserved")
	}
}

func newMemStore(t *testing.T) Store {
	t.Helper()
	st, err := NewMemStore(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	return st
}
