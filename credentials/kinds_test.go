package credentials

import (
	"testing"
)

func TestUnmarshalUsernamePassword(t *testing.T) {
	data, err := MarshalUsernamePassword("alice", "secret")
	if err != nil {
		t.Fatal(err)
	}
	u, p, err := UnmarshalUsernamePassword(data)
	if err != nil {
		t.Fatal(err)
	}
	if u != "alice" || p != "secret" {
		t.Fatalf("got %q %q", u, p)
	}
}

func TestUnmarshalUsernamePasswordEmptyPassword(t *testing.T) {
	b := []byte(`{"username":"x","password":""}`)
	_, _, err := UnmarshalUsernamePassword(b)
	if err == nil {
		t.Fatal("expected error")
	}
}
