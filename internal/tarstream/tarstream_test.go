package tarstream

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "world.txt"), []byte("world"), 0o600); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		if err := os.Symlink("hello.txt", filepath.Join(root, "hello-link")); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	if err := WriteDirectory(context.Background(), &buf, root, "photos"); err != nil {
		t.Fatal(err)
	}

	tr := tar.NewReader(bytes.NewReader(buf.Bytes()))
	got := map[string]string{}
	links := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if hdr.Typeflag == tar.TypeSymlink {
			links[hdr.Name] = hdr.Linkname
			continue
		}
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == tar.TypeRegA {
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			got[hdr.Name] = string(b)
		}
	}

	if got["photos/hello.txt"] != "hello" {
		t.Fatalf("missing hello.txt: %#v", got)
	}
	if got["photos/sub/world.txt"] != "world" {
		t.Fatalf("missing world.txt: %#v", got)
	}
	if runtime.GOOS != "windows" && links["photos/hello-link"] != "hello.txt" {
		t.Fatalf("symlink not preserved: %#v", links)
	}
}

func TestCancelledContext(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var buf bytes.Buffer
	if err := WriteDirectory(ctx, &buf, root, "x"); err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestCancelledDuringFile(t *testing.T) {
	root := t.TempDir()
	data := bytes.Repeat([]byte("a"), 256*1024)
	if err := os.WriteFile(filepath.Join(root, "large"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writer := &cancelOnData{cancel: cancel}
	if err := WriteDirectory(ctx, writer, root, "archive"); err != context.Canceled {
		t.Fatalf("got %v, want context.Canceled", err)
	}
	if writer.n <= 1024 || writer.n >= len(data) {
		t.Fatalf("expected part of the file to be written, got %d bytes", writer.n)
	}
}

type cancelOnData struct {
	n      int
	cancel context.CancelFunc
}

func (w *cancelOnData) Write(p []byte) (int, error) {
	w.n += len(p)
	if w.n > 1024 {
		w.cancel()
	}
	return len(p), nil
}
