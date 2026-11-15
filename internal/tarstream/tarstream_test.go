package tarstream

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveContainsFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	var archive bytes.Buffer
	if err := WriteDirectory(context.Background(), &archive, dir, "photos"); err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(&archive)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			t.Fatal("archive does not contain photos/hello.txt")
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Name == "photos/hello.txt" {
			contents, err := io.ReadAll(reader)
			if err != nil || string(contents) != "hello" {
				t.Fatalf("file contents = %q, error = %v", contents, err)
			}
			return
		}
	}
}

func TestPlanMatchesArchiveAndSupportsSeeking(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	plan, complete, err := PlanBounded(context.Background(), root, "photos", nil)
	if err != nil || !complete {
		t.Fatalf("plan complete = %v, error = %v", complete, err)
	}
	defer plan.Close()
	var archive bytes.Buffer
	if err := WriteRoot(context.Background(), &archive, root, "photos", nil); err != nil {
		t.Fatal(err)
	}
	if plan.Size() != int64(archive.Len()) {
		t.Fatalf("planned size = %d, archive size = %d", plan.Size(), archive.Len())
	}
	if _, err := plan.Seek(512, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	remainder, err := io.ReadAll(plan)
	if err != nil || !bytes.Equal(remainder, archive.Bytes()[512:]) {
		t.Fatalf("seeked archive does not match, error = %v", err)
	}
}
