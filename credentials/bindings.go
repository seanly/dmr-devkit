package credentials

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// MaxBindings is the maximum number of credential bindings per tool call.
const MaxBindings = 32

// Binding maps one credential to environment variable(s).
// For simple types (secretText, secretFile): use Env field.
// For composite types (usernamePassword, sshPrivateKey): use EnvMap field.
type Binding struct {
	CredentialID string            `json:"credential_id"`
	Env          string            `json:"env,omitempty"`
	EnvMap       map[string]string `json:"env_map,omitempty"`
}

// ParseBindings parses credential_bindings from a JSON-decoded tool argument.
func ParseBindings(raw any) ([]Binding, error) {
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("credentials: credential_bindings must be an array")
	}
	if len(arr) > MaxBindings {
		return nil, fmt.Errorf("credentials: credential_bindings exceeds max (%d)", MaxBindings)
	}
	var bindings []Binding
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("credentials: credential_bindings item must be an object")
		}
		b := Binding{
			CredentialID: strings.TrimSpace(strVal(coalesceAny(m["credential_id"], m["credentialId"]))),
		}
		if b.CredentialID == "" {
			return nil, fmt.Errorf("credentials: credential_bindings item missing credential_id")
		}
		b.Env = strings.TrimSpace(strVal(m["env"]))
		b.EnvMap = parseStringMap(coalesceAny(m["env_map"], m["envMap"]))

		if b.Env == "" && len(b.EnvMap) == 0 {
			return nil, fmt.Errorf("credentials: credential_bindings item %q must specify env or env_map", b.CredentialID)
		}
		bindings = append(bindings, b)
	}
	return bindings, nil
}

// ResolveBindings resolves credential bindings against the store.
// Returns env vars to inject and temp file paths for cleanup.
func ResolveBindings(ctx context.Context, store Store, bindings []Binding) (env map[string]string, tempFiles []string, err error) {
	env = make(map[string]string)
	for _, b := range bindings {
		c, err := store.Get(ctx, b.CredentialID)
		if err != nil {
			return nil, tempFiles, fmt.Errorf("credentials: %q: %w", b.CredentialID, err)
		}

		switch c.Kind {
		case KindSecretText:
			if b.Env == "" {
				cleanupFiles(tempFiles)
				return nil, nil, fmt.Errorf("credentials: %q is secretText, use env (not env_map)", b.CredentialID)
			}
			env[b.Env] = string(c.Data)

		case KindSecretFile:
			if b.Env == "" {
				cleanupFiles(tempFiles)
				return nil, nil, fmt.Errorf("credentials: %q is secretFile, use env (not env_map)", b.CredentialID)
			}
			f, err := WriteSecretTempFile(c.Data)
			if err != nil {
				cleanupFiles(tempFiles)
				return nil, nil, err
			}
			env[b.Env] = f
			tempFiles = append(tempFiles, f)

		case KindUsernamePassword:
			if len(b.EnvMap) == 0 {
				cleanupFiles(tempFiles)
				return nil, nil, fmt.Errorf("credentials: %q is usernamePassword, use env_map (not env)", b.CredentialID)
			}
			username, password, err := UnmarshalUsernamePassword(c.Data)
			if err != nil {
				cleanupFiles(tempFiles)
				return nil, nil, fmt.Errorf("credentials: %q: %w", b.CredentialID, err)
			}
			for field, envName := range b.EnvMap {
				envName = strings.TrimSpace(envName)
				if envName == "" {
					continue
				}
				switch field {
				case "username", "Username":
					env[envName] = username
				case "password", "Password":
					env[envName] = password
				default:
					cleanupFiles(tempFiles)
					return nil, nil, fmt.Errorf("credentials: %q unknown usernamePassword field %q", b.CredentialID, field)
				}
			}

		case KindSSHPrivateKey:
			if len(b.EnvMap) == 0 {
				cleanupFiles(tempFiles)
				return nil, nil, fmt.Errorf("credentials: %q is sshPrivateKey, use env_map (not env)", b.CredentialID)
			}
			privateKey, keyUsername, passphrase, err := UnmarshalSSHPrivateKey(c.Data)
			if err != nil {
				cleanupFiles(tempFiles)
				return nil, nil, fmt.Errorf("credentials: %q: %w", b.CredentialID, err)
			}
			for field, envName := range b.EnvMap {
				envName = strings.TrimSpace(envName)
				if envName == "" {
					continue
				}
				switch field {
				case "username", "Username":
					env[envName] = keyUsername
				case "privateKey", "PrivateKey", "private_key", "privatekey":
					f, err := WriteSecretTempFile([]byte(privateKey))
					if err != nil {
						cleanupFiles(tempFiles)
						return nil, nil, err
					}
					env[envName] = f
					tempFiles = append(tempFiles, f)
				case "passphrase", "Passphrase":
					env[envName] = passphrase
				default:
					cleanupFiles(tempFiles)
					return nil, nil, fmt.Errorf("credentials: %q unknown sshPrivateKey field %q", b.CredentialID, field)
				}
			}

		default:
			cleanupFiles(tempFiles)
			return nil, nil, fmt.Errorf("credentials: %q unsupported kind %q", b.CredentialID, c.Kind)
		}
	}
	return env, tempFiles, nil
}

func cleanupFiles(paths []string) {
	for _, p := range paths {
		if p != "" {
			_ = os.Remove(p)
		}
	}
}

func strVal(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func coalesceAny(a, b any) any {
	if s := strings.TrimSpace(strVal(a)); s != "" {
		return a
	}
	return b
}

func parseStringMap(v any) map[string]string {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, val := range m {
		s := strings.TrimSpace(strVal(val))
		if s != "" {
			result[k] = s
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
