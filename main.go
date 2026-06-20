// Command auranode es el CLI del panel AuraNode.
package main

import (
	"os"

	"github.com/koyere/auranode-cli/cmd"
)

// version se sobreescribe en build con -ldflags "-X main.version=X.Y.Z".
var version = "dev"

func main() {
	cmd.SetVersion(version)
	os.Exit(cmd.Execute())
}
