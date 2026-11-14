package tarstream

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanMatchesStreamingAndSeeks(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "season"), 0755); err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("a", 120)
	data := bytes.Repeat([]byte("episode"), 500)
	if err := os.WriteFile(filepath.Join(dir, "season", name), data, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "season", name), filepath.Join(dir, "season", "z-hardlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(name, filepath.Join(dir, "season", "z-symlink")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hidden"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	skip := func(name string) bool { return name == "hidden" }
	plan, complete, err := PlanBounded(context.Background(), root, "series", skip)
	if err != nil || !complete {
		t.Fatalf("plan: %v %v", complete, err)
	}
	defer plan.Close()
	var expected bytes.Buffer
	if err := WriteRoot(context.Background(), &expected, root, "series", skip); err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected.Bytes()) || plan.Size() != int64(expected.Len()) {
		t.Fatalf("plan differs: %d/%d", len(actual), expected.Len())
	}
	for _, off := range []int64{0, 511, 512, 1025, int64(expected.Len() / 2), int64(expected.Len() - 1)} {
		if _, err := plan.Seek(off, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		chunk := make([]byte, 97)
		n, err := io.ReadFull(plan, chunk)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			t.Fatal(err)
		}
		if !bytes.Equal(chunk[:n], expected.Bytes()[off:off+int64(n)]) {
			t.Fatalf("range differs at %d", off)
		}
	}
	other, complete, err := PlanBounded(context.Background(), root, "series", skip)
	if err != nil || !complete || other.ETag() != plan.ETag() {
		t.Fatal("validator changed for unchanged directory")
	}
	other.Close()
	if err := os.WriteFile(filepath.Join(dir, "season", name), bytes.Repeat([]byte("y"), len(data)), 0644); err != nil {
		t.Fatal(err)
	}
	changed, complete, err := PlanBounded(context.Background(), root, "series", skip)
	if err != nil || !complete || changed.ETag() == plan.ETag() {
		t.Fatal("validator did not change")
	}
	changed.Close()
	if _, err := plan.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(plan); err == nil {
		t.Fatal("changed file was streamed")
	}
	if err := os.Mkdir(filepath.Join(dir, "season", "deep"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, complete, err := PlanBounded(context.Background(), root, "series", skip); err != nil || complete {
		t.Fatalf("deep directory: %v %v", complete, err)
	}
}

func TestPlanRejectsFileModifiedDuringStream(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, bytes.Repeat([]byte("x"), 4096), 0644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	plan, complete, err := PlanBounded(context.Background(), root, "folder", nil)
	if err != nil || !complete {
		t.Fatalf("plan: %v %v", complete, err)
	}
	defer plan.Close()
	var start int64 = -1
	for _, segment := range plan.segments {
		if segment.file != "" {
			start = segment.start
			break
		}
	}
	if start < 0 {
		t.Fatal("missing file segment")
	}
	if _, err := plan.Seek(start, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	if _, err := plan.Read(make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, bytes.Repeat([]byte("y"), 4096), 0644); err != nil {
		t.Fatal(err)
	}
	if n, err := plan.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatalf("changed data accepted: %d %v", n, err)
	}
}
