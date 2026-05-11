package credentials

import (
	"encoding/json"
	"fmt"
	"strings"
)

type usernamePasswordPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type sshPrivateKeyPayload struct {
	Username   string `json:"username,omitempty"`
	PrivateKey string `json:"privateKey"`
	Passphrase string `json:"passphrase,omitempty"`
}

// MarshalUsernamePassword returns JSON bytes for KindUsernamePassword.
func MarshalUsernamePassword(username, password string) ([]byte, error) {
	return json.Marshal(usernamePasswordPayload{Username: username, Password: password})
}

// MarshalSSHPrivateKey returns JSON bytes for KindSSHPrivateKey.
func MarshalSSHPrivateKey(username, privateKeyPEM, passphrase string) ([]byte, error) {
	return json.Marshal(sshPrivateKeyPayload{
		Username:   username,
		PrivateKey: privateKeyPEM,
		Passphrase: passphrase,
	})
}

// UnmarshalUsernamePassword parses JSON produced by MarshalUsernamePassword (KindUsernamePassword Data field).
func UnmarshalUsernamePassword(data []byte) (username, password string, err error) {
	var p usernamePasswordPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return "", "", err
	}
	if strings.TrimSpace(p.Password) == "" {
		return "", "", fmt.Errorf("credentials: usernamePassword payload missing password")
	}
	return p.Username, p.Password, nil
}

// UnmarshalSSHPrivateKey parses JSON produced by MarshalSSHPrivateKey (KindSSHPrivateKey Data field).
func UnmarshalSSHPrivateKey(data []byte) (privateKeyPEM, keyUsername, passphrase string, err error) {
	var p sshPrivateKeyPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return "", "", "", err
	}
	if strings.TrimSpace(p.PrivateKey) == "" {
		return "", "", "", fmt.Errorf("credentials: sshPrivateKey payload missing privateKey")
	}
	return p.PrivateKey, p.Username, p.Passphrase, nil
}
