//go:build !windows

package ui

import (
	"fmt"
	"os"
)

// checkGraphics is a Windows-only concern; macOS always has OpenGL.
func checkGraphics() error { return nil }

func showFatal(msg string) { fmt.Fprintln(os.Stderr, msg) }
