package bridge

import (
	"fmt"
	"net/url"
	"strings"
)

const defaultConnectPath = "/api/plugin/localbridge/connect"

// ResolveWSURL converts a server origin or full WS URL into a WebSocket connect URL.
func ResolveWSURL(server string) (string, error) {
	server = strings.TrimSpace(server)
	if server == "" {
		return "", fmt.Errorf("bridge server URL is required")
	}

	if strings.HasPrefix(server, "ws://") || strings.HasPrefix(server, "wss://") {
		u, err := url.Parse(server)
		if err != nil {
			return "", fmt.Errorf("parse server URL: %w", err)
		}
		if u.Path == "" || u.Path == "/" {
			u.Path = defaultConnectPath
		}
		return u.String(), nil
	}

	u, err := url.Parse(server)
	if err != nil {
		return "", fmt.Errorf("parse server URL: %w", err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported server scheme %q", u.Scheme)
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = defaultConnectPath
	}
	return u.String(), nil
}
