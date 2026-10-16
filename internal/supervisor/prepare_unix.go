//go:build !windows

package supervisor

func PrepareProcess() error { return nil }
