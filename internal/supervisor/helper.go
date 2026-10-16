package supervisor

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// RunHelper is the privileged child. It starts sshd and cloudflared, then
// stops both when the GUI writes a shutdown file or the GUI process disappears.
func RunHelper(args []string) {
	fs := flag.NewFlagSet("helper", flag.ContinueOnError)
	dir := fs.String("dir", "", "runtime directory")
	if err := fs.Parse(args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "missing -dir")
		os.Exit(2)
	}
	os.Exit(runHelper(*dir))
}

func runHelper(dir string) int {
	logf := newLogger(dir)
	data, err := os.ReadFile(RequestPath(dir))
	if err != nil {
		logf("读取请求失败: " + err.Error())
		writeStatus(dir, Status{State: "error", Detail: err.Error()})
		return 1
	}
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		logf("请求格式不对: " + err.Error())
		writeStatus(dir, Status{State: "error", Detail: err.Error()})
		return 1
	}
	redacted := req
	redacted.TunnelToken = ""
	_ = WriteRequest(dir, redacted)
	if fileExists(ShutdownPath(dir)) || !processAlive(req.ParentPID) {
		logf("启动前已收到停止信号")
		writeStatus(dir, Status{State: "stopped", Detail: "已停止"})
		return 0
	}
	// The token is on disk only until cloudflared has started.
	defer os.Remove(tokenPath(dir))

	writeStatus(dir, Status{State: "starting", Detail: "正在启动 SSH", Port: req.Port})
	logf(fmt.Sprintf("启动 SSH，监听 %s:%d", req.Listen, req.Port))
	sshProc, err := startSSHD(dir, req, logf)
	if err != nil {
		msg := "SSH 启动失败: " + err.Error()
		logf(msg)
		writeStatus(dir, Status{State: "error", Detail: msg, SSH: "error", Port: req.Port})
		return 1
	}
	defer sshProc.stop()

	writeStatus(dir, Status{State: "starting", Detail: "正在连接 Cloudflare", SSH: "running", Port: req.Port})
	cfProc, err := startCloudflared(dir, req, logf)
	if err != nil {
		msg := "cloudflared 启动失败: " + err.Error()
		logf(msg)
		writeStatus(dir, Status{State: "error", Detail: msg, SSH: "running", Tunnel: "error", Port: req.Port})
		return 1
	}
	defer cfProc.stop()
	_ = os.Remove(tokenPath(dir))

	writeStatus(dir, Status{
		State:  "running",
		Detail: req.Hostname,
		SSH:    "running",
		Tunnel: "running",
		Port:   req.Port,
	})
	logf("隧道和 SSH 都已运行。关闭窗口后会停止。")

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if fileExists(ShutdownPath(dir)) || !processAlive(req.ParentPID) {
			logf("收到停止信号，正在关闭隧道和 SSH")
			break
		}
		if !sshProc.alive() {
			logf("SSH 进程退出了")
			writeStatus(dir, Status{State: "error", Detail: "SSH 进程退出了", SSH: "stopped", Tunnel: "running", Port: req.Port})
			break
		}
		if !cfProc.alive() {
			logf("cloudflared 进程退出了")
			writeStatus(dir, Status{State: "error", Detail: "cloudflared 进程退出了", SSH: "running", Tunnel: "stopped", Port: req.Port})
			break
		}
	}
	cfProc.stop()
	sshProc.stop()
	writeStatus(dir, Status{State: "stopped", Detail: "已停止", SSH: "stopped", Tunnel: "stopped", Port: req.Port})
	logf("已停止")
	return 0
}

func startCloudflared(dir string, req Request, logf func(string)) (*proc, error) {
	if req.Cloudflared == "" || req.TunnelToken == "" {
		return nil, fmt.Errorf("缺少 cloudflared 或隧道令牌")
	}
	reapPrevious(dir, "cloudflared.pid", "cloudflared", logf)
	path := tokenPath(dir)
	if err := os.WriteFile(path, []byte(req.TunnelToken), 0o600); err != nil {
		return nil, err
	}
	cmd := exec.Command(req.Cloudflared,
		"tunnel", "--no-autoupdate", "--loglevel", "info",
		"run", "--token-file", path,
		"--metrics", "127.0.0.1:20245",
	)
	configureCommand(cmd)
	if err := attachLog(cmd, logf); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	pidFile := filepath.Join(dir, "cloudflared.pid")
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
	logf("cloudflared pid " + strconv.Itoa(cmd.Process.Pid))
	return &proc{
		cmd: cmd,
		stopFn: func() {
			stopProcess(cmd)
			_ = os.Remove(pidFile)
		},
	}, nil
}

func tokenPath(dir string) string { return filepath.Join(dir, "tunnel.token") }

func reapPrevious(dir, file, needle string, logf func(string)) {
	data, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || !processAlive(pid) {
		return
	}
	if needle != "" && !strings.Contains(strings.ToLower(commandLine(pid)), strings.ToLower(needle)) {
		return
	}
	logf("停掉上次残留的 " + needle + " (" + strconv.Itoa(pid) + ")")
	killPID(pid)
}

func combinedOutputWhileParent(parent int, dir string, cmd *exec.Cmd) ([]byte, error) {
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	configureCommand(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case err := <-done:
			return buf.Bytes(), err
		case <-ticker.C:
			if (parent > 1 && !processAlive(parent)) || (dir != "" && fileExists(ShutdownPath(dir))) {
				killPID(cmd.Process.Pid)
				err := <-done
				if err == nil {
					err = errProcExited
				}
				return buf.Bytes(), err
			}
		}
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func newLogger(dir string) func(string) {
	return func(msg string) {
		line := time.Now().Format("15:04:05 ") + strings.TrimRight(msg, "\n") + "\n"
		f, err := os.OpenFile(LogPath(dir), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprint(os.Stderr, line)
			return
		}
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
}

func attachLog(cmd *exec.Cmd, logf func(string)) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	go copyLog(stdout, logf)
	go copyLog(stderr, logf)
	return nil
}
