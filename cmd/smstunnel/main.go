// Command smstunnel sends texts from your computer through your own Android phone and number, with SMSTunnel
// (https://smstunnel.io). Run "smstunnel --help".
package main

import (
	"os"

	// Time zones come with the binary, so local times match the Node.js version (which has its own) even where
	// the system has no time zone files.
	_ "time/tzdata"

	"github.com/narcisbodea/smstunnel-cli/internal/cli"
)

func main() {
	os.Exit(cli.Main())
}
