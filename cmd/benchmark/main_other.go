//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "benchmark: the stable research benchmark requires Linux telemetry")
	os.Exit(1)
}
