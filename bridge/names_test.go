package bridge

import "testing"

func TestFormatToolName(t *testing.T) {
	got := FormatToolName("local", "sean-macbook", "fsRead")
	want := "local_sean-macbook_fsRead"
	if got != want {
		t.Fatalf("FormatToolName() = %q, want %q", got, want)
	}
}

func TestFormatToolNameDefaultPrefix(t *testing.T) {
	got := FormatToolName("", "w1", "shell")
	if got != "local_w1_shell" {
		t.Fatalf("default prefix = %q", got)
	}
}

func TestParseToolName(t *testing.T) {
	wid, orig, ok := ParseToolName("local", "local_sean-macbook_fsRead")
	if !ok || wid != "sean-macbook" || orig != "fsRead" {
		t.Fatalf("ParseToolName() = %q %q %v", wid, orig, ok)
	}
}

func TestParseToolNameMCP(t *testing.T) {
	wid, orig, ok := ParseToolName("local", "local_w1_mcp_filesystem_read_file")
	if !ok || wid != "w1" || orig != "mcp_filesystem_read_file" {
		t.Fatalf("ParseToolName() = %q %q %v", wid, orig, ok)
	}
}

func TestParseToolNameInvalid(t *testing.T) {
	if _, _, ok := ParseToolName("local", "fsRead"); ok {
		t.Fatal("expected false for non-prefixed name")
	}
	if _, _, ok := ParseToolName("local", "local_onlyone"); ok {
		t.Fatal("expected false for missing original segment")
	}
}

func TestFormatParseRoundTrip(t *testing.T) {
	full := FormatToolName("local", "alice", "shell")
	wid, orig, ok := ParseToolName("local", full)
	if !ok || wid != "alice" || orig != "shell" {
		t.Fatalf("round trip failed: %q %q %v", wid, orig, ok)
	}
}
