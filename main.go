// Command auranode is the CLI for the AuraNode panel.
package main

import (
	"os"

	"github.com/koyere/auranode-cli/cmd"
)

// version is overridden at build time with -ldflags "-X main.version=X.Y.Z".
var version = "dev"

func main() {
	cmd.SetVersion(version)
	os.Exit(cmd.Execute())
}
