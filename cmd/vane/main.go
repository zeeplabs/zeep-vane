package main

import (
	"os"
	_ "time/tzdata" // embed IANA tzdata so LoadLocation works on minimal containers without host tzdata

	"github.com/zeeplabs/zeep-vane/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
