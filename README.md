# Caddy file server with tar downloads

Give visitors a **Download folder (.tar)** link in Caddy's file browser. Archives are streamed directly from disk; Caddy does not create a temporary tar file.

```caddyfile
files.example.com {
    root * /srv/files
    file_server_with_tar {
        browse
        hide .git .env
    }
}
```

Open `https://files.example.com/photos/` to browse the folder, or download it directly:

```sh
curl -OJ 'https://files.example.com/photos/?download=tar'
```

Normal file requests are served by Caddy's built-in file server. `file_server_with_tar` accepts the usual `file_server` options, including custom browse templates. For a custom template, add a link to `?download=tar`, or start with the included [browse.html](browse.html).

## Build Caddy

From a local checkout, build Caddy with the plugin using [xcaddy](https://github.com/caddyserver/xcaddy):

```sh
xcaddy build --with "github.com/fantattitude/caddy-file-server-with-tar=$PWD"
./caddy list-modules | grep http.handlers.folder_tar
```

For a private repository, clone it with SSH before building. Caddy 2.11.9 or later is required.

## Downloads and sizes

- Archives include a top-level folder, preserve symbolic links and hard links, and omit paths matching `hide`. Dotfiles are included unless hidden explicitly.
- For a folder containing files and at most one level of subfolders, the server calculates an exact `Content-Length` and supports byte ranges with a matching `If-Range` ETag. Deeper folders still stream, without a known length or resume support.
- `GET /photos/?download=tar-size` returns JSON such as `{"bytes": 123456, "complete": true}`. If the folder is too deep or the required metadata is unavailable, `complete` is `false`. The response is privately cached for one hour; each actual download is planned again.
- Resume validation uses filesystem metadata, not a content hash or snapshot. Changing files during a download can interrupt it, and metadata cannot detect every possible in-place change.

Custom browse templates can use `placeholder (printf "folder_tar.show.%s" .Name)` to check whether to show a folder download link. It returns `"false"` for a folder with only one visible file or one that cannot be inspected.

Tar downloads require the default local filesystem on Linux, macOS, or FreeBSD. The requested folder may be a symlink; symlinks *inside* an archive are preserved, not traversed. Folder downloads use the directory route's access controls, so per-file URL matchers do not filter archive contents. Use `hide` to exclude paths.

## Development

Requires Go 1.26 or later:

```sh
go test ./...
```
