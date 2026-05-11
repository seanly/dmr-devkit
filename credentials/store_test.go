package credentials

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func testStoreCRUD(t *testing.T, st Store) {
	t.Helper()
	ctx := context.Background()
	id := "test.cred-1"

	_, err := st.Get(ctx, id)
	if err == nil {
		t.Fatal("expected error before put")
	}

	data, err := MarshalUsernamePassword("u", "p")
	if err != nil {
		t.Fatal(err)
	}
	c := Credential{ID: id, Kind: KindUsernamePassword, Data: data, Description: "d1"}
	if err := st.Put(ctx, c); err != nil {
		t.Fatal(err)
	}

	got, err := st.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != KindUsernamePassword || string(got.Data) != string(data) {
		t.Fatalf("get mismatch: kind=%s data=%q", got.Kind, got.Data)
	}

	list, err := st.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != id {
		t.Fatalf("list: %+v", list)
	}

	if err := st.Delete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get(ctx, id); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestMemStorePlaintext(t *testing.T) {
	st, err := NewMemStore(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	testStoreCRUD(t, st)
}

func TestFileStorePlaintext(t *testing.T) {
	root := t.TempDir()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := NewFileStore(abs, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	testStoreCRUD(t, st)
}

func TestMemStoreEncrypted(t *testing.T) {
	dek := make([]byte, 32)
	for i := range dek {
		dek[i] = byte(i + 1)
	}
	st, err := NewMemStore(dek, false)
	if err != nil {
		t.Fatal(err)
	}
	testStoreCRUD(t, st)
}

func TestValidateID(t *testing.T) {
	if err := ValidateID(""); err == nil {
		t.Fatal("empty id")
	}
	if err := ValidateID("../x"); err == nil {
		t.Fatal("path chars")
	}
	if err := ValidateID("ok.id-1"); err != nil {
		t.Fatal(err)
	}
}

func TestFileStore_ListSkipsUnreadableAndCorrupted(t *testing.T) {
	root := t.TempDir()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	st, err := NewFileStore(abs, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Put a valid credential
	data, _ := MarshalUsernamePassword("u", "p")
	valid := Credential{ID: "valid.cred", Kind: KindUsernamePassword, Data: data}
	if err := st.Put(ctx, valid); err != nil {
		t.Fatal(err)
	}

	// Create an unreadable file (no read permission)
	unreadablePath := filepath.Join(root, "unreadable.json")
	if err := os.WriteFile(unreadablePath, []byte("{}"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(unreadablePath, 0o600) // ensure cleanup

	// Create a corrupted JSON file
	corruptedPath := filepath.Join(root, "corrupted.json")
	if err := os.WriteFile(corruptedPath, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}

	list, err := st.List(ctx)
	if err != nil {
		t.Fatalf("unexpected error from List: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("expected 1 valid credential after skipping bad files, got %d", len(list))
	}
	if len(list) > 0 && list[0].ID != "valid.cred" {
		t.Errorf("expected valid.cred, got %s", list[0].ID)
	}
}
