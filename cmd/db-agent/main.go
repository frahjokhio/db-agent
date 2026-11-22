// db-agent: safe database tools for your AI, as one executable file.
package main

import (
	"os"

	"github.com/frahjokhio/db-agent/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
