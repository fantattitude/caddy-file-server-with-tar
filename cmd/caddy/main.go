// Command caddy builds Caddy with its standard modules and the tar handler.
package main

import (
	_ "time/tzdata"

	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	_ "github.com/caddyserver/caddy/v2/modules/standard"
	_ "github.com/fantattitude/caddy-file-server-with-tar"
)

func main() {
	caddycmd.Main()
}
