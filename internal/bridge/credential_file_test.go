package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialDeletionPreservesChangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	original, updated := []byte(`{"disabled":true}`), []byte(`{"disabled":false}`)
	if err := os.WriteFile(path, updated, 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeCredentialFile(path, original); statusOf(err) != 409 {
		t.Fatalf("expected an external edit conflict, got %v", err)
	}
	if current, err := os.ReadFile(path); err != nil || string(current) != string(updated) {
		t.Fatal("external credential edit was not preserved")
	}
}
