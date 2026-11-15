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

## Install with Nix

Add the flake input, using SSH for a private repository:

```nix
inputs.caddy-file-server-with-tar = {
  url = "git+ssh://git@github.com/fantattitude/caddy-file-server-with-tar.git?ref=main";
  inputs.nixpkgs.follows = "nixpkgs";
};
```

For a Caddy installation without other plugins, add the module to your NixOS host's module list and enable Caddy:

```nix
modules = [
  inputs.caddy-file-server-with-tar.nixosModules.default
  { services.caddy.enable = true; }
];
```

The package currently omits Caddy's upstream systemd units, which some nixpkgs NixOS module versions expect. Those versions need the units added to the package before enabling the service.

To use other Caddy plugins in the same binary, use the builder in a NixOS module instead:

```nix
{ inputs, lib, pkgs, ... }: {
  services.caddy.enable = true;
  services.caddy.package = inputs.caddy-file-server-with-tar.lib.buildCaddy pkgs {
    extraPlugins = [ "github.com/caddy-dns/cloudflare@v0.2.4" ];
    sourceHash = lib.fakeHash;
  };
}
```

Build once, then replace `lib.fakeHash` with the reported `got:` hash. Refresh that hash when the plugin source, plugin list, or Caddy version changes. The builder defaults to `pkgs.caddy.version` (2.11.9 or later); set `caddyVersion` to override it. Its standalone default package has a bundled hash for Caddy 2.11.9; use the builder and a fresh `sourceHash` for another version. Check compatibility when upgrading Caddy.

The flake supports `x86_64-linux`, `aarch64-linux`, and `aarch64-darwin`. To build the default package from a checkout, run `nix build` and use `result/bin/caddy`.

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
go build -o caddy ./cmd/caddy
./caddy list-modules | grep http.handlers.folder_tar
./caddy validate --config Caddyfile.example --adapter caddyfile
```

To include the plugin in another Caddy build from a local checkout:

```sh
xcaddy build v2.11.9 \
  --with "github.com/fantattitude/caddy-file-server-with-tar=$PWD"
```
