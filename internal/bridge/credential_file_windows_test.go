package bridge

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCredentialDeletionWaitsForWindowsReader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credential.json")
	original := []byte(`{"disabled":true}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	closed := make(chan struct{})
	go func() {
		time.Sleep(100 * time.Millisecond)
		reader.Close()
		close(closed)
	}()
	err = removeCredentialFile(path, original)
	<-closed
	if err != nil {
		t.Fatalf("temporary Windows reader prevented deletion: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("credential still exists: %v", err)
	}
}
