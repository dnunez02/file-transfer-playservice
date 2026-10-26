package filesource

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidName(t *testing.T) {
	for name, want := range map[string]bool{
		"a.txt": true, "large-file.txt": true,
		"": false, "../x": false, "a/b": false, ".hidden": false, "a..b": false,
	} {
		if got := ValidName(name); got != want {
			t.Errorf("ValidName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestDirFetch(t *testing.T) {
	folder := t.TempDir()
	os.WriteFile(filepath.Join(folder, "a.txt"), []byte("hello"), 0o644)
	dir := Dir{Path: folder}

	if data, err := dir.Fetch("a.txt"); err != nil || string(data) != "hello" {
		t.Errorf("Fetch(a.txt) = %q, %v", data, err)
	}
	if _, err := dir.Fetch("missing.txt"); err == nil {
		t.Error("Fetch of a missing file succeeded")
	}
	if _, err := dir.Fetch("../a.txt"); err == nil {
		t.Error("Fetch of a path outside the folder succeeded")
	}
}
