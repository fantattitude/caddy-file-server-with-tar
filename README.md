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

Normal file requests are served by Caddy's built-in file server. `file_server_with_tar` accepts the usual `file_server` options, including custom browse templates. For a custom template, add a link to `?download=tar`.

## Build Caddy

From a local checkout, build Caddy with the plugin using [xcaddy](https://github.com/caddyserver/xcaddy):

```sh
xcaddy build --with "github.com/fantattitude/caddy-file-server-with-tar=$PWD"
./caddy list-modules | grep http.handlers.folder_tar
```

For a private repository, clone it with SSH before building. Caddy 2.11.9 or later is required.

## Downloads and sizes

The tar includes the folder itself and preserves symlinks and hard links. It includes dotfiles too, unless they're covered by a `hide` rule. Links inside the folder aren't followed.

For folders with at most one level of subfolders, downloads have a known size and can resume when the client sends a matching `If-Range` ETag. Deeper folders still download, but without a size or resume support. Files changing during a download can interrupt it; the resume check uses metadata, not file hashes.

`?download=tar-size` returns JSON like `{"bytes": 123456, "complete": true}`. The size can be cached for an hour. If the folder is too deep or metadata isn't available, `complete` is `false`.

Custom templates can use `placeholder (printf "folder_tar.show.%s" .Name)` to hide the download link for single-file or unreadable folders.

## Development

Requires Go 1.26 or later:

```sh
go test ./...
```
