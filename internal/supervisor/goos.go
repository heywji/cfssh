package supervisor

func Alive(pid int) bool { return processAlive(pid) }
