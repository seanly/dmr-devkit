package bridge

import (
	"encoding/json"
	"fmt"
)

// Encode marshals a frame to JSON bytes.
func Encode(f Frame) ([]byte, error) {
	return json.Marshal(f)
}

// Decode unmarshals JSON bytes into a frame.
func Decode(data []byte) (Frame, error) {
	var f Frame
	if err := json.Unmarshal(data, &f); err != nil {
		return Frame{}, fmt.Errorf("decode bridge frame: %w", err)
	}
	if f.Type == "" {
		return Frame{}, fmt.Errorf("decode bridge frame: missing type")
	}
	return f, nil
}

// MustEncode encodes a frame or panics (for tests).
func MustEncode(f Frame) []byte {
	b, err := Encode(f)
	if err != nil {
		panic(err)
	}
	return b
}
