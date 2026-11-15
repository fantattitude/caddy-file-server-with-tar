package foldertar

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFolderTarAvailable(t *testing.T) {
	dir := t.TempDir()
	if got := folderTarAvailable(dir, nil); got != "true" {
		t.Fatalf("empty folder: %s", got)
	}
	for _, name := range []string{"first", "second", "third"} {
		if err := os.Symlink("missing", filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if got := folderTarAvailable(dir, nil); got != "false" {
		t.Fatalf("only symlinks: %s", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := folderTarAvailable(dir, nil); got != "true" {
		t.Fatalf("mixed entries: %s", got)
	}
	if got := folderTarAvailable(dir, []string{"file"}); got != "false" {
		t.Fatalf("hidden regular file: %s", got)
	}
}
