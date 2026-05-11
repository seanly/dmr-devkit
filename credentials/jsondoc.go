package credentials

import "time"

const fileVersion = 1

func parseRFC3339(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Parse(time.RFC3339, s)
	}
	return t, nil
}

// diskDoc is the on-disk JSON envelope (per credential file).
type diskDoc struct {
	Version     int               `json:"version"`
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	UpdatedAt   string            `json:"updated_at"`
	Description string            `json:"description,omitempty"`
	Meta        map[string]string `json:"meta,omitempty"`
	Encrypted   bool              `json:"encrypted"`
	NonceB64    string            `json:"nonce_b64,omitempty"`
	PayloadB64  string            `json:"payload_b64,omitempty"`
}

func metaFromDoc(d *diskDoc) CredentialMeta {
	t, err := parseRFC3339(d.UpdatedAt)
	if err != nil {
		t = time.Time{}
	}
	m := CredentialMeta{
		ID:          d.ID,
		Kind:        d.Kind,
		Description: d.Description,
		UpdatedAt:   t,
		Meta:        d.Meta,
	}
	if m.Meta == nil {
		m.Meta = make(map[string]string)
	}
	return m
}
