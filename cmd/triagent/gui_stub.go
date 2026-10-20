//go:build !gui

package main

import (
	"fmt"
	"os"
)

func runGUI() {
	fmt.Fprintln(os.Stderr, "triagent: GUI support was not compiled into this binary.")
	fmt.Fprintln(os.Stderr, "Rebuild with: go build -tags gui ./cmd/triagent")
	os.Exit(1)
}
