package bridge

import "testing"

func TestEncodeDecodeRoundTrip(t *testing.T) {
	in := Frame{
		Type:     TypeRegister,
		WorkerID: "sean-macbook",
		Tools: []ToolDef{{
			Name:           "local_sean-macbook_fsRead",
			Description:    "read file",
			ParametersJSON: `{"type":"object"}`,
			Route:          Route{Plugin: "fs", OriginalName: "fsRead"},
		}},
	}
	data, err := Encode(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if out.WorkerID != in.WorkerID || len(out.Tools) != 1 {
		t.Fatalf("decode mismatch: %+v", out)
	}
}

func TestDecodeMissingType(t *testing.T) {
	if _, err := Decode([]byte(`{"worker_id":"x"}`)); err == nil {
		t.Fatal("expected error for missing type")
	}
}
