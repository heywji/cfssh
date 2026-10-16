//go:build !windows

package ui

import (
	"fmt"
	"os"
)

// checkGraphics is a Windows-only concern; macOS and Linux desktops have OpenGL.
func checkGraphics() error { return nil }

func showFatal(msg string) { fmt.Fprintln(os.Stderr, msg) }
