package supervisor

import "runtime"

func goos() string { return runtime.GOOS }

func Alive(pid int) bool { return processAlive(pid) }
