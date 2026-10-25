//go:build !linux

package main

import (
	"fmt"
	"os"
)

func cmdOverlay(_ GlobalFlags, _ []string) int {
	fmt.Fprintln(os.Stderr, "overlay: mount namespaces and OverlayFS require Linux")

	return 2
}
