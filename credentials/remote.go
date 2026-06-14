package credentials

import (
	"context"
	"fmt"
	"strings"
)

// RemoteFile carries secret file bytes for worker-side materialization.
type RemoteFile struct {
	Env     string // inject temp path as this env var value
	Target  string // or set args[target] to temp path (e.g. ssh_key_path)
	Content []byte
}

// ResolveBindingsRemote resolves bindings without writing temp files on the caller side.
func ResolveBindingsRemote(ctx context.Context, store Store, bindings []Binding) (env map[string]string, files []RemoteFile, err error) {
	env = make(map[string]string)
	for _, b := range bindings {
		c, err := store.Get(ctx, b.CredentialID)
		if err != nil {
			return nil, files, fmt.Errorf("credentials: %q: %w", b.CredentialID, err)
		}

		switch c.Kind {
		case KindSecretText:
			if b.Env == "" {
				return nil, nil, fmt.Errorf("credentials: %q is secretText, use env (not env_map)", b.CredentialID)
			}
			env[b.Env] = string(c.Data)

		case KindSecretFile:
			if b.Env == "" {
				return nil, nil, fmt.Errorf("credentials: %q is secretFile, use env (not env_map)", b.CredentialID)
			}
			files = append(files, RemoteFile{Env: b.Env, Content: append([]byte(nil), c.Data...)})

		case KindUsernamePassword:
			if len(b.EnvMap) == 0 {
				return nil, nil, fmt.Errorf("credentials: %q is usernamePassword, use env_map (not env)", b.CredentialID)
			}
			username, password, err := UnmarshalUsernamePassword(c.Data)
			if err != nil {
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
					return nil, nil, fmt.Errorf("credentials: %q unknown usernamePassword field %q", b.CredentialID, field)
				}
			}

		case KindSSHPrivateKey:
			if len(b.EnvMap) == 0 {
				return nil, nil, fmt.Errorf("credentials: %q is sshPrivateKey, use env_map (not env)", b.CredentialID)
			}
			privateKey, keyUsername, passphrase, err := UnmarshalSSHPrivateKey(c.Data)
			if err != nil {
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
					files = append(files, RemoteFile{Env: envName, Content: []byte(privateKey)})
				case "passphrase", "Passphrase":
					env[envName] = passphrase
				default:
					return nil, nil, fmt.Errorf("credentials: %q unknown sshPrivateKey field %q", b.CredentialID, field)
				}
			}

		default:
			return nil, nil, fmt.Errorf("credentials: %q unsupported kind %q", b.CredentialID, c.Kind)
		}
	}
	return env, files, nil
}

// ResolveSSHCredential loads an sshPrivateKey credential for remote worker injection.
func ResolveSSHCredential(ctx context.Context, store Store, credentialID string) ([]byte, error) {
	credentialID = strings.TrimSpace(credentialID)
	if credentialID == "" {
		return nil, fmt.Errorf("credentials: empty credential id")
	}
	c, err := store.Get(ctx, credentialID)
	if err != nil {
		return nil, fmt.Errorf("credentials: %q: %w", credentialID, err)
	}
	if c.Kind != KindSSHPrivateKey {
		return nil, fmt.Errorf("credentials: %q is %q, want sshPrivateKey", credentialID, c.Kind)
	}
	privateKey, _, _, err := UnmarshalSSHPrivateKey(c.Data)
	if err != nil {
		return nil, fmt.Errorf("credentials: %q: %w", credentialID, err)
	}
	return []byte(privateKey), nil
}
