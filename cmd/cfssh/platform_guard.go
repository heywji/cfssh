//go:build !windows && !darwin

package main

// cfssh supports Windows and macOS only. This undefined name makes a build for
// any other platform fail at compile time instead of producing a broken binary.
var _ = cfssh_supports_only_windows_and_macos
