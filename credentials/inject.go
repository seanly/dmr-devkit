package credentials

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	RuntimeEnvKey           = "_runtime_env"
	RuntimeTempFilesKey     = "_runtime_inject_tempfiles"
	RuntimeBridgeFilesKey   = "_runtime_bridge_files"
	SSHCredentialIDArg      = "ssh_credential_id"
	SSHCredentialIDArgCamel = "sshCredentialId"
	SSHCredentialTarget     = "ssh_key_path"
)

// bridgeFileWire is the JSON shape for _runtime_bridge_files in tool args.
type bridgeFileWire struct {
	Env        string `json:"env,omitempty"`
	Target     string `json:"target,omitempty"`
	ContentB64 string `json:"content_b64"`
}

// ApplyRemoteBindings merges remote credential resolution into args for bridge forwarding.
func ApplyRemoteBindings(args map[string]any, env map[string]string, files []RemoteFile) error {
	if args == nil {
		return nil
	}
	if len(env) > 0 {
		existing := getOrCreateRuntimeEnvMap(args)
		for k, v := range env {
			existing[k] = v
		}
		args[RuntimeEnvKey] = existing
	}
	if len(files) > 0 {
		wire := make([]bridgeFileWire, 0, len(files))
		for _, f := range files {
			if len(f.Content) == 0 {
				continue
			}
			wire = append(wire, bridgeFileWire{
				Env:        strings.TrimSpace(f.Env),
				Target:     strings.TrimSpace(f.Target),
				ContentB64: base64.StdEncoding.EncodeToString(f.Content),
			})
		}
		if len(wire) > 0 {
			args[RuntimeBridgeFilesKey] = wire
		}
	}
	return nil
}

// ResolveRemoteCredentialRefs resolves tool-native credential references for remote execution.
func ResolveRemoteCredentialRefs(ctx context.Context, store Store, toolName string, args map[string]any) error {
	if args == nil || store == nil {
		return nil
	}
	switch strings.TrimSpace(toolName) {
	case "sshExec", "sshUpload", "sshDownload":
		credID := strings.TrimSpace(strVal(args[SSHCredentialIDArg]))
		if credID == "" {
			credID = strings.TrimSpace(strVal(args[SSHCredentialIDArgCamel]))
		}
		if credID == "" {
			return nil
		}
		pem, err := ResolveSSHCredential(ctx, store, credID)
		if err != nil {
			return err
		}
		return ApplyRemoteBindings(args, nil, []RemoteFile{{Target: SSHCredentialTarget, Content: pem}})
	default:
		return nil
	}
}

// SanitizeWireArgs removes credential references before sending args to a worker.
func SanitizeWireArgs(args map[string]any) {
	if args == nil {
		return
	}
	delete(args, "credential_bindings")
	delete(args, SSHCredentialIDArg)
	delete(args, SSHCredentialIDArgCamel)
	delete(args, RuntimeTempFilesKey)
}

// MaterializeBridgeInject writes bridge file payloads on the worker and returns a cleanup func.
func MaterializeBridgeInject(args map[string]any) (cleanup func(), err error) {
	if args == nil {
		return func() {}, nil
	}
	var tempFiles []string
	raw, ok := args[RuntimeBridgeFilesKey]
	if ok && raw != nil {
		items, err := parseBridgeFiles(raw)
		if err != nil {
			return func() {}, err
		}
		for _, item := range items {
			if len(item.Content) == 0 {
				continue
			}
			path, err := WriteSecretTempFile(item.Content)
			if err != nil {
				cleanupFiles(tempFiles)
				return func() {}, err
			}
			tempFiles = append(tempFiles, path)
			if item.Target != "" {
				args[item.Target] = path
			}
			if item.Env != "" {
				existing := getOrCreateRuntimeEnvMap(args)
				existing[item.Env] = path
				args[RuntimeEnvKey] = existing
			}
		}
		delete(args, RuntimeBridgeFilesKey)
	}
	if len(tempFiles) > 0 {
		appendRuntimeTempFiles(args, tempFiles)
	}
	return func() { cleanupFiles(tempFiles) }, nil
}

func getOrCreateRuntimeEnvMap(args map[string]any) map[string]string {
	if m, ok := args[RuntimeEnvKey].(map[string]string); ok && m != nil {
		out := make(map[string]string, len(m)+1)
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	if m, ok := args[RuntimeEnvKey].(map[string]any); ok && m != nil {
		out := make(map[string]string, len(m)+1)
		for k, v := range m {
			out[k] = fmt.Sprint(v)
		}
		return out
	}
	return make(map[string]string)
}

func appendRuntimeTempFiles(args map[string]any, paths []string) {
	var list []string
	switch t := args[RuntimeTempFilesKey].(type) {
	case []string:
		list = append(append([]string{}, t...), paths...)
	case []any:
		for _, x := range t {
			list = append(list, fmt.Sprint(x))
		}
		list = append(list, paths...)
	default:
		list = append([]string{}, paths...)
	}
	args[RuntimeTempFilesKey] = list
}

type parsedBridgeFile struct {
	Env     string
	Target  string
	Content []byte
}

func parseBridgeFiles(raw any) ([]parsedBridgeFile, error) {
	switch arr := raw.(type) {
	case []bridgeFileWire:
		return decodeBridgeWireSlice(arr)
	case []any:
		var wire []bridgeFileWire
		for _, item := range arr {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("credentials: bridge file item must be object")
			}
			wire = append(wire, bridgeFileWire{
				Env:        strVal(m["env"]),
				Target:     strVal(m["target"]),
				ContentB64: strVal(m["content_b64"]),
			})
		}
		return decodeBridgeWireSlice(wire)
	default:
		return nil, nil
	}
}

func decodeBridgeWireSlice(wire []bridgeFileWire) ([]parsedBridgeFile, error) {
	out := make([]parsedBridgeFile, 0, len(wire))
	for _, w := range wire {
		if strings.TrimSpace(w.ContentB64) == "" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(w.ContentB64)
		if err != nil {
			return nil, fmt.Errorf("credentials: decode bridge file: %w", err)
		}
		out = append(out, parsedBridgeFile{
			Env:     strings.TrimSpace(w.Env),
			Target:  strings.TrimSpace(w.Target),
			Content: b,
		})
	}
	return out, nil
}

// CloneArgsForWire returns a shallow copy of args suitable for marshaling to a worker.
func CloneArgsForWire(args map[string]any) map[string]any {
	if args == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	SanitizeWireArgs(out)
	return out
}
