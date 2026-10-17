//go:build darwin

package supervisor

import (
	"os/exec"
	"testing"
	"time"
)

func TestProcessAliveTreatsZombieAsGone(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	// Not waited for yet, so the exited child lingers as a zombie.
	deadline := time.Now().Add(5 * time.Second)
	for processAlive(cmd.Process.Pid) {
		if time.Now().After(deadline) {
			t.Fatal("an exited, unreaped child still reports alive")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestProcessAliveRunningChild(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	if !processAlive(cmd.Process.Pid) {
		t.Fatal("a running child must report alive")
	}
}
