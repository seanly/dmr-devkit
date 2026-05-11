package credentials

import (
	"bytes"
	"testing"
)

func TestEncryptPayloadRoundTrip(t *testing.T) {
	dek := make([]byte, 32)
	for i := range dek {
		dek[i] = byte(i + 3)
	}
	plain := []byte("hello credential payload")
	nb, pb, err := EncryptPayload(dek, plain)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecryptPayload(dek, nb, pb)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, plain) {
		t.Fatalf("got %q want %q", out, plain)
	}
}
