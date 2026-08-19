package main

import (
	"os"

	"github.com/zeeplabs/zeep-vane/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
