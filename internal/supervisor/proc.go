package supervisor

import (
	"os/exec"
	"sync"
	"time"
)

// proc is one child, or a service, owned by the helper.
type proc struct {
	once    sync.Once
	cmd     *exec.Cmd
	aliveFn func() bool
	stopFn  func()
}

func (p *proc) alive() bool {
	if p == nil {
		return false
	}
	if p.aliveFn != nil {
		return p.aliveFn()
	}
	if p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	return processAlive(p.cmd.Process.Pid)
}

func (p *proc) stop() {
	if p == nil {
		return
	}
	p.once.Do(func() {
		if p.stopFn != nil {
			p.stopFn()
			return
		}
		stopProcess(p.cmd)
	})
}

func waitListen(addr string, timeout time.Duration, alive func() bool) error {
	deadline := time.Now().Add(timeout)
	var last error
	for time.Now().Before(deadline) {
		if alive != nil && !alive() {
			return errProcExited
		}
		conn, err := dial(addr)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		last = err
		time.Sleep(200 * time.Millisecond)
	}
	if last == nil {
		last = errTimeout
	}
	return last
}
