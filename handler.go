package foldertar

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"go.uber.org/zap"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/fileserver"
	"github.com/fantattitude/caddy-file-server-with-tar/internal/tarstream"
)

func init() {
	// The stock template only shows the control while this handler delegates
	// to its own native file server. Custom browse templates still take precedence.
	const layout = `<a id="layout-list"`
	const download = `{{if eq (placeholder "folder_tar.browse") "true"}}<a class="layout" href="?download=tar">Download folder (.tar)</a>{{end}}`
	if strings.Contains(fileserver.BrowseTemplate, layout) {
		fileserver.BrowseTemplate = strings.Replace(fileserver.BrowseTemplate, layout, download+layout, 1)
	} else {
		fileserver.BrowseTemplate = strings.Replace(fileserver.BrowseTemplate, "</main>", download+"</main>", 1)
	}
	caddy.RegisterModule(Handler{})
	httpcaddyfile.RegisterDirective("file_server_with_tar", parseCaddyfile)
	httpcaddyfile.RegisterDirectiveOrder("file_server_with_tar", httpcaddyfile.Before, "file_server")
}

// Handler intercepts directory requests with ?download=tar and streams an
// uncompressed tar. All other requests continue to Caddy's built-in file_server.
type Handler struct {
	Root       string   `json:"root,omitempty"`
	FileSystem string   `json:"fs,omitempty"`
	Hide       []string `json:"hide,omitempty"`
	QueryParam string   `json:"query_param,omitempty"`
	QueryValue string   `json:"query_value,omitempty"`
	Browse     bool     `json:"browse,omitempty"`

	logger      *zap.Logger
	fileSystems caddy.FileSystems
	osFS        fs.FS
}

func (Handler) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{ID: "http.handlers.folder_tar", New: func() caddy.Module { return new(Handler) }}
}

func (h *Handler) Provision(ctx caddy.Context) error {
	h.logger = ctx.Logger()
	h.fileSystems = ctx.FileSystems()
	var empty caddy.Context
	h.osFS = empty.FileSystems().Default()
	// Match the native file server's runtime protection for fallback data dirs.
	if dataDir := caddy.AppDataDir(); !filepath.IsAbs(dataDir) {
		if abs, err := caddy.FastAbs(dataDir); err == nil {
			h.Hide = append(h.Hide, abs)
		}
	}
	if h.Root == "" {
		h.Root = "{http.vars.root}"
	}
	if h.FileSystem == "" {
		h.FileSystem = "{http.vars.fs}"
	}
	if h.QueryParam == "" {
		h.QueryParam = "download"
	}
	if h.QueryValue == "" {
		h.QueryValue = "tar"
	}
	return nil
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)
	fsName := repl.ReplaceAll(h.FileSystem, "")
	fileSystem, _ := h.fileSystems.Get(fsName)
	localFS := fileSystem == h.osFS && (runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "freebsd")
	if r.URL.Query().Get(h.QueryParam) != h.QueryValue {
		if h.Browse && localFS {
			repl.Set("folder_tar.browse", "true")
			defer repl.Delete("folder_tar.browse")
		}
		return next.ServeHTTP(w, r)
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		return caddyhttp.Error(http.StatusMethodNotAllowed, nil)
	}

	if !localFS {
		return caddyhttp.Error(http.StatusNotImplemented, fmt.Errorf("tar downloads require the default local filesystem"))
	}

	root := repl.ReplaceAll(h.Root, ".")
	filename := filepath.Clean(caddyhttp.SanitizedPathJoin(root, r.URL.Path))
	hide := transformHidePaths(h.Hide, repl)
	if fileHidden(filename, hide) {
		return caddyhttp.Error(http.StatusNotFound, nil)
	}

	rel, err := filepath.Rel(root, filename)
	if err != nil {
		return caddyhttp.Error(http.StatusInternalServerError, err)
	}
	base, err := os.OpenRoot(root)
	if err != nil {
		return directoryError(err)
	}
	defer base.Close()
	directory, err := openDirectory(base, rel)
	if err != nil {
		return directoryError(err)
	}
	defer directory.Close()

	archiveRoot := path.Base(path.Clean("/" + r.URL.Path))
	if archiveRoot == "/" {
		archiveRoot = "archive"
	}

	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": archiveRoot + ".tar"}))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return nil
	}

	w.WriteHeader(http.StatusOK)
	err = tarstream.WriteRoot(r.Context(), w, directory, archiveRoot, func(name string) bool {
		return fileHidden(filepath.Join(filename, name), hide)
	})
	if err != nil {
		if r.Context().Err() != nil {
			return nil
		}
		h.logger.Error("streaming directory tar failed", zap.String("directory", filename), zap.Error(err))
		// A status and possibly archive data have already been sent.
		panic(http.ErrAbortHandler)
	}
	return nil
}

var errNotDirectory = errors.New("tar download target is not a directory")

// Check each path component so a symlink can't be used as a download URL,
// even if the link points to another directory within the configured root.
func openDirectory(base *os.Root, rel string) (*os.Root, error) {
	dir, err := base.OpenRoot(".")
	if err != nil || rel == "." {
		return dir, err
	}
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		next, err := openSubdirectory(dir, component)
		dir.Close()
		if err != nil {
			return nil, err
		}
		dir = next
	}
	return dir, nil
}

func openSubdirectory(parent *os.Root, name string) (*os.Root, error) {
	info, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, os.ErrNotExist
	}
	if !info.IsDir() {
		return nil, errNotDirectory
	}
	return tarstream.OpenDirectory(parent, name, info)
}

func directoryError(err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return caddyhttp.Error(http.StatusNotFound, err)
	case errors.Is(err, os.ErrPermission):
		return caddyhttp.Error(http.StatusForbidden, err)
	case errors.Is(err, errNotDirectory):
		return caddyhttp.Error(http.StatusBadRequest, err)
	default:
		return caddyhttp.Error(http.StatusInternalServerError, err)
	}
}

// parseCaddyfile deliberately delegates parsing of the directive body to
// Caddy's built-in FileServer parser. We then emit a single route containing
// our tar handler followed by the real built-in file_server handler.
func parseCaddyfile(h httpcaddyfile.Helper) ([]httpcaddyfile.ConfigValue, error) {
	if !h.Next() {
		return nil, h.ArgErr()
	}
	matcherSet, err := h.ExtractMatcherSet()
	if err != nil {
		return nil, err
	}

	fsrv := new(fileserver.FileServer)
	if err := fsrv.UnmarshalCaddyfile(h.Dispenser); err != nil {
		return nil, err
	}
	if err := fsrv.FinalizeUnmarshalCaddyfile(h); err != nil {
		return nil, err
	}

	tarHandler := Handler{
		Root: fsrv.Root, FileSystem: fsrv.FileSystem, Hide: fsrv.Hide,
		Browse: fsrv.Browse != nil && fsrv.Browse.TemplateFile == "",
	}

	tarRaw := caddyconfig.JSONModuleObject(&tarHandler, "handler", "folder_tar", nil)
	fileRaw := caddyconfig.JSONModuleObject(fsrv, "handler", "file_server", nil)
	var matcherSets []caddy.ModuleMap
	if matcherSet != nil {
		matcherSets = append(matcherSets, matcherSet)
	}

	return []httpcaddyfile.ConfigValue{{
		Class: "route",
		Value: caddyhttp.Route{MatcherSetsRaw: matcherSets, HandlersRaw: []json.RawMessage{tarRaw, fileRaw}},
	}}, nil
}

func transformHidePaths(in []string, repl *caddy.Replacer) []string {
	out := make([]string, len(in))
	sep := string(filepath.Separator)
	for i, p := range in {
		out[i] = repl.ReplaceAll(p, "")
		if strings.Contains(out[i], sep) {
			if abs, err := caddy.FastAbs(out[i]); err == nil {
				out[i] = abs
			}
		}
	}
	return out
}

// Mirrors Caddy 2.11's file_server hide matching semantics.
// Adapted from modules/caddyhttp/fileserver/staticfiles.go, copyright 2015
// Matthew Holt and The Caddy Authors, licensed under Apache-2.0.
func fileHidden(filename string, hide []string) bool {
	if len(hide) == 0 {
		return false
	}
	if abs, err := caddy.FastAbs(filename); err == nil {
		filename = abs
	}
	sep := string(filepath.Separator)
	var components []string
	for _, h := range hide {
		if !strings.Contains(h, sep) {
			if components == nil {
				components = strings.Split(filename, sep)
			}
			for _, c := range components {
				if ok, _ := filepath.Match(h, c); ok {
					return true
				}
			}
		} else if after, ok := strings.CutPrefix(filename, h); ok && strings.HasPrefix(after, sep) {
			return true
		}
		if ok, _ := filepath.Match(h, filename); ok {
			return true
		}
	}
	return false
}

var (
	_ caddy.Provisioner           = (*Handler)(nil)
	_ caddyhttp.MiddlewareHandler = (*Handler)(nil)
)
