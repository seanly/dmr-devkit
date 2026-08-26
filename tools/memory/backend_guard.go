package memory

// requireBackend returns (nil, false) when the backend is ready.
// Otherwise returns a structured tool result and true — callers should `return resp, nil`.
func (p *Service) requireBackend() (map[string]any, bool) {
	if p == nil || p.backend == nil {
		return map[string]any{
			"success": false,
			"error":   "memory backend not initialized (plugin init failed or memory is disabled)",
		}, true
	}
	return nil, false
}
