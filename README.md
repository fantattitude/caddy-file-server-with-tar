# Caddy file server with tar downloads

Use `file_server_with_tar` in place of Caddy's native `file_server` where you want directory downloads. Caddy parses the usual `file_server` options and serves ordinary requests itself.

```caddyfile
files.example.com {
	root * /srv/files
	file_server_with_tar {
		browse
		hide .git .env
	}
}
```

`GET /photos/?download=tar` streams `photos.tar` without creating a temporary archive. `HEAD` returns the download headers. The built-in HTML listing adds a **Download folder (.tar)** link. With a custom browse template you can add `href="?download=tar"` yourself or use the supplied `browse.html`.

`?download=tar-size` returns the exact planned tar size for the current directory and its immediate subdirectories, including tar headers and hardlinks. It returns `complete: false` when a deeper directory is encountered or the filesystem cannot supply the required metadata. Successful responses are privately cached for one hour. The directory can change after an estimate is cached. Custom browse templates can check `placeholder (printf "folder_tar.show.%s" .Name)` for each directory; it is `"false"` for a folder containing only one visible file, or when the folder cannot be inspected. This reads up to two visible entries per directory during page rendering.

For the same bounded directory depth, downloads are planned from metadata and streamed directly from the original files without a temporary tar. Responses include the exact `Content-Length`, `Accept-Ranges: bytes`, and a metadata-derived ETag. A `Range` request with a matching `If-Range` validator seeks directly to the requested archive offset; a missing or stale validator restarts with a full `200` response. Deeper directories retain normal streaming without a length or range support. Plans are recalculated for every request, so the browser's cached size is not used to frame the download.

The validator and read-time checks use filesystem identity, change time, modification time, size, mode, and generated tar headers—not file-content hashes or an immutable snapshot. Ordinary file changes cause a restart or interrupt the stream, but metadata alone cannot guarantee detection of every in-place change. Resume assumes files remain unchanged while downloading. The response may terminate if a file changes or becomes unreadable during streaming.

## Nix

The flake pins Caddy 2.11.9 and uses `nixpkgs` for the Go toolchain and build environment. It exports `packages.<system>.default` on `x86_64-linux`, `aarch64-linux` and `aarch64-darwin`. A local checkout can be built with `nix build`, which produces `result/bin/caddy`.

Add the input to your NixOS flake:

```nix
inputs.caddy-file-server-with-tar = {
  url = "github:fantattitude/caddy-file-server-with-tar";
  inputs.nixpkgs.follows = "nixpkgs";
};
```

Import `inputs.caddy-file-server-with-tar.nixosModules.default` in the host's module list to set `services.caddy.package` by default. The module leaves `services.caddy.enable` under your control. You can also assign `inputs.caddy-file-server-with-tar.packages.${pkgs.stdenv.hostPlatform.system}.default` to `services.caddy.package` directly. For private GitHub repositories, Nix needs GitHub credentials or the flake input can use the SSH URL `git+ssh://git@github.com/fantattitude/caddy-file-server-with-tar.git`.

If you already build Caddy with additional plugins, use one binary that includes all of them. The builder takes Go module paths pinned to a tag or canonical pseudo-version:

```nix
services.caddy.package = inputs.caddy-file-server-with-tar.lib.buildCaddy pkgs {
  extraPlugins = [ "github.com/caddy-dns/cloudflare@v0.2.4" ];
  sourceHash = lib.fakeHash;
};
```

Build once and replace `lib.fakeHash` with the `got:` hash from Nix. Recompute it after changing the plugin list, Caddy version, or local plugin source. The optional `caddyVersion` argument accepts `2.11.9` or later. The builder checks that Go resolves the requested Caddy and plugin versions. Go resolves shared library dependencies across the entire build, so an older dependency pin may need to move forward.

## Go

Requires Go 1.26 or later. To build and verify from a checkout:

```sh
go test ./...
go build -o caddy ./cmd/caddy
./caddy list-modules | grep http.handlers.folder_tar
./caddy validate --config Caddyfile.example --adapter caddyfile
```

To include the plugin in another Caddy build from a local checkout:

```sh
xcaddy build v2.11.9 \
  --with "github.com/fantattitude/caddy-file-server-with-tar=$PWD"
```

## Archive behavior and scope

- Tar requests use the default local filesystem on Linux, macOS, and FreeBSD. They return HTTP 501 for other filesystems or platforms. Native requests still use Caddy's configured filesystem.
- The archive omits entries matched by `hide`, including Caddyfiles the adapter hides automatically. Hidden directories are pruned. The handler mirrors Caddy 2.11's hide matching. Recheck it when changing the Caddy version.
- An archive contains a top-level directory named after the requested URL directory (`archive/` at the site root). The requested directory path follows symlinks like Caddy's native file server, including aliases whose targets are outside the site root. The selected directory is opened and pinned before streaming. Inside the archive, symlink entries preserve their link targets, including targets outside the share; archive traversal never follows those entries. Preserve link targets only where disclosing them is acceptable.
- Regular files and directories are included. Sockets, devices, and FIFOs are skipped. Tar downloads follow the access controls on the directory route, so per-file URL matchers inside that directory do not filter the archive. Use `hide` to exclude archive contents.
- Hardlinked regular files included in the archive share one content copy; later paths are tar hard-link entries referring to the first visible archived path. The size estimate counts their contents once. Open descriptors for the first occurrence of each hardlinked file remain pinned until the stream ends to prevent inode reuse from linking unrelated files.
- The archive reads the live directory. Changes or read errors during generation can interrupt a download. Extraction should use a tar tool that handles untrusted symlink targets safely.
