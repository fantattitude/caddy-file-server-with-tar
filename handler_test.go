package foldertar

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func TestTarDownloadThroughSymlink(t *testing.T) {
	share := symlinkShare(t)
	for _, test := range []struct{ path, archiveRoot string }{
		{"/Series/Season%201/", "Season 1"},
		{"/Chain/Season%201/", "Season 1"},
		{"/Local/", "Local"},
		{"/Plain/", "Plain"},
		{"/Absolute/", "Absolute"},
	} {
		t.Run(test.path, func(t *testing.T) {
			rec, err := tarRequest(t, share, http.MethodGet, test.path+"?download=tar", []string{"hidden.txt", ".private"})
			if err != nil {
				t.Fatal(err)
			}
			if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/x-tar" {
				t.Fatalf("unexpected response: %d %v", rec.Code, rec.Header())
			}
			mediaType, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
			if err != nil || mediaType != "attachment" {
				t.Fatalf("invalid download header: %q (%v)", rec.Header().Get("Content-Disposition"), err)
			}
			archiveRoot := test.archiveRoot
			if params["filename"] != archiveRoot+".tar" {
				t.Fatalf("filename = %q", params["filename"])
			}
			reader := tar.NewReader(bytes.NewReader(rec.Body.Bytes()))
			seenFile, seenLink := false, false
			for {
				header, err := reader.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				switch header.Name {
				case archiveRoot + "/":
				case archiveRoot + "/visible.txt":
					content, err := io.ReadAll(reader)
					if err != nil || string(content) != "hello" {
						t.Fatalf("unexpected content: %q (%v)", content, err)
					}
					seenFile = true
				case archiveRoot + "/external":
					if header.Typeflag != tar.TypeSymlink || header.Linkname != "../../outside" {
						t.Fatalf("symlink was not preserved: %#v", header)
					}
					seenLink = true
				default:
					t.Fatalf("unexpected archive entry (hidden file or followed symlink): %q", header.Name)
				}
			}
			if !seenFile || !seenLink {
				t.Fatalf("missing file or symlink: file=%v link=%v", seenFile, seenLink)
			}
		})
	}
}

func TestFolderTarAvailable(t *testing.T) {
	dir := t.TempDir()
	if got := folderTarAvailable(dir, nil); got != "true" {
		t.Fatalf("empty: %s", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "one"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := folderTarAvailable(dir, nil); got != "false" {
		t.Fatalf("one file: %s", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "hidden"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := folderTarAvailable(dir, []string{"hidden"}); got != "false" {
		t.Fatalf("hidden file: %s", got)
	}
	if got := folderTarAvailable(dir, nil); got != "true" {
		t.Fatalf("two files: %s", got)
	}
	if err := os.Remove(filepath.Join(dir, "one")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "hidden")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := folderTarAvailable(dir, nil); got != "true" {
		t.Fatalf("one directory: %s", got)
	}
}

func TestTarSymlinkHeadAndErrors(t *testing.T) {
	share := symlinkShare(t)
	rec, err := tarRequest(t, share, http.MethodHead, "/Series/Season%201/?download=tar", nil)
	if err != nil || rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Disposition") == "" {
		t.Fatalf("HEAD failed: code=%d bytes=%d headers=%v error=%v", rec.Code, rec.Body.Len(), rec.Header(), err)
	}
	for _, test := range []struct {
		name string
		path string
		hide []string
		code int
	}{
		{"hidden-alias", "/Series/Season%201/", []string{"Series"}, http.StatusNotFound},
		{"hidden-child", "/Series/Season%201/.private/", []string{".private"}, http.StatusNotFound},
		{"missing", "/Series/Missing/", nil, http.StatusNotFound},
		{"broken-link", "/Broken/", nil, http.StatusNotFound},
		{"file", "/Series/Season%201/visible.txt", nil, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := tarRequest(t, share, http.MethodGet, test.path+"?download=tar", test.hide)
			var handlerError caddyhttp.HandlerError
			if !errors.As(err, &handlerError) || handlerError.StatusCode != test.code {
				t.Fatalf("got %v, want HTTP %d", err, test.code)
			}
		})
	}
}

func symlinkShare(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("tar downloads require a supported Unix filesystem")
	}
	base := t.TempDir()
	share := filepath.Join(base, "share")
	season := filepath.Join(base, "media", "Season 1")
	for _, dir := range []string{season, filepath.Join(share, "Plain"), filepath.Join(base, "outside")} {
		if err := os.MkdirAll(filepath.Join(dir, ".private"), 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"visible.txt", "hidden.txt", filepath.Join(".private", "secret.txt")} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("hello"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, dir := range []string{season, filepath.Join(share, "Plain")} {
		if err := os.Symlink("../../outside", filepath.Join(dir, "external")); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{
		"Series":   "../media",
		"Chain":    "Series",
		"Local":    "Plain",
		"Absolute": season,
		"Broken":   "missing",
	} {
		if err := os.Symlink(target, filepath.Join(share, name)); err != nil {
			t.Fatal(err)
		}
	}
	return share
}

func tarRequest(t *testing.T, root, method, uri string, hide []string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	var ctx caddy.Context
	fileSystems := ctx.FileSystems()
	handler := Handler{
		Root: root, Hide: hide, QueryParam: "download", QueryValue: "tar",
		logger: zap.NewNop(), fileSystems: fileSystems, osFS: fileSystems.Default(),
	}
	request := httptest.NewRequest(method, "http://files.example"+uri, nil)
	request = request.WithContext(context.WithValue(request.Context(), caddy.ReplacerCtxKey, caddy.NewReplacer()))
	recorder := httptest.NewRecorder()
	err := handler.ServeHTTP(recorder, request, caddyhttp.HandlerFunc(func(http.ResponseWriter, *http.Request) error {
		t.Fatal("tar request unexpectedly reached the native file server")
		return nil
	}))
	return recorder, err
}
