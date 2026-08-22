// Command downmonitor is the command-line client for Downmonitor.
package main

import (
	"os"

	"github.com/downmonitor/cli/internal/api"
	"github.com/downmonitor/cli/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	cli.Version = version
	api.Version = version

	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
