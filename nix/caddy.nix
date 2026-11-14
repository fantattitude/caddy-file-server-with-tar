{ lib, buildGoModule, go, stdenvNoCC, cacert }:
{
  caddyVersion ? "2.11.9",
  extraPlugins ? [ ],
  sourceHash ? lib.fakeHash,
}:
let
  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [ ../go.mod ../go.sum ../handler.go ../cmd ../internal ];
  };
  custom = extraPlugins != [ ] || caddyVersion != "2.11.9";
  specs = lib.sort builtins.lessThan extraPlugins;
  pluginImports = lib.concatMapStringsSep "\n" (spec: "\t_ ${builtins.toJSON (lib.head (lib.splitString "@" spec))}") specs;
  prepared = stdenvNoCC.mkDerivation {
    name = "caddy-with-tar-${caddyVersion}-source";
    inherit src;
    nativeBuildInputs = [ go cacert ];
    dontConfigure = true;
    dontFixup = true;
    env.GOTOOLCHAIN = "local";
    buildPhase = ''
      runHook preBuild
      export GOPATH="$TMPDIR/go" GOCACHE="$TMPDIR/go-cache"
      cat > cmd/caddy/plugins.go <<'GO'
      package main
      ${lib.optionalString (specs != [ ]) ''
      import (
      ${pluginImports}
      )
      ''}
      GO
      go get ${lib.escapeShellArgs ([ "github.com/caddyserver/caddy/v2@v${caddyVersion}" ] ++ specs)}
      test "$(go list -m -f '{{.Version}}' github.com/caddyserver/caddy/v2)" = ${lib.escapeShellArg "v${caddyVersion}"}
      ${lib.concatMapStringsSep "\n" (spec:
        let parts = lib.splitString "@" spec; in
        ''test "$(go list -m -f '{{.Version}}' ${lib.escapeShellArg (lib.head parts)})" = ${lib.escapeShellArg (lib.last parts)}''
      ) specs}
      go mod tidy
      go mod vendor
      runHook postBuild
    '';
    installPhase = ''
      runHook preInstall
      mkdir -p "$out"
      cp -r . "$out/"
      runHook postInstall
    '';
    outputHashMode = "recursive";
    outputHash = sourceHash;
  };
in
assert lib.assertMsg (lib.versionAtLeast caddyVersion "2.11.9") "Caddy with tar requires Caddy 2.11.9 or later";
assert lib.assertMsg (lib.all (spec: builtins.match "[^@=]+@v[^@=]+" spec != null) specs)
  "Each extra Caddy plugin must use module@vVERSION (including canonical Go pseudo-versions)";
buildGoModule {
  pname = "caddy-with-tar";
  version = caddyVersion;
  src = if custom then prepared else src;
  vendorHash = if custom then null else "sha256-P93c/bRHZCOK5G3ZYkphd/wZsSpCApeqS1fxo1RyEHs=";
  subPackages = [ "cmd/caddy" ];
  tags = [ "nobadger" "nomysql" "nopgx" ];
  ldflags = [ "-s" "-w" "-X github.com/caddyserver/caddy/v2.CustomVersion=${caddyVersion}" ];
  meta = {
    description = "Caddy with streamed tar downloads for directories";
    homepage = "https://github.com/fantattitude/caddy-file-server-with-tar";
    license = lib.licenses.asl20;
    mainProgram = "caddy";
  };
}
