package handoff

import "testing"

func TestParseStateJSON(t *testing.T) {
	base := NewState("book tickets", "llm_extract")
	raw := `{"goal":"book Paris tickets","constraints":{"seat":"aisle"},"last_action":"fsRead loop.go"}`
	got, err := ParseStateJSON(raw, base)
	if err != nil {
		t.Fatal(err)
	}
	if got.Goal != "book Paris tickets" {
		t.Fatalf("goal = %q", got.Goal)
	}
	if got.Constraints["seat"] != "aisle" {
		t.Fatalf("constraints = %v", got.Constraints)
	}
}
