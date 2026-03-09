package main

import (
	"os"

	"github.com/hybridz/yap/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
