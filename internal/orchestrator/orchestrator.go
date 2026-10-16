package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"cfssh/internal/auth"
	"cfssh/internal/cfapi"
	"cfssh/internal/clientcfg"
	"cfssh/internal/cloudflared"
	"cfssh/internal/config"
	"cfssh/internal/supervisor"
)

var ErrStopped = errors.New("已停止")

var userRE = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

type Orchestrator struct {
	mu      sync.Mutex
	logs    []string
	helper  *exec.Cmd
	runDir  string
	changed func()
}

func (o *Orchestrator) OnChange(fn func()) {
	o.mu.Lock()
	o.changed = fn
	o.mu.Unlock()
}

func (o *Orchestrator) Log(msg string) {
	o.mu.Lock()
	o.logs = append(o.logs, time.Now().Format("15:04:05 ")+msg)
	if len(o.logs) > 2000 {
		o.logs = o.logs[len(o.logs)-2000:]
	}
	fn := o.changed
	o.mu.Unlock()
	if fn != nil {
		fn()
	}
}

func (o *Orchestrator) Text() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.Join(o.logs, "\n")
}

func (o *Orchestrator) RunDir() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.runDir
}

func (o *Orchestrator) Running() bool {
	o.mu.Lock()
	helper := o.helper
	dir := o.runDir
	o.mu.Unlock()
	if helper != nil && helper.Process != nil && supervisor.Alive(helper.Process.Pid) {
		st, err := supervisor.ReadStatus(dir)
		if err == nil && st.State == "running" {
			return true
		}
	}
	return false
}

// TestToken checks the API token and returns the accounts and zones it can see.
func TestToken(ctx context.Context, token string) ([]cfapi.Account, []cfapi.Zone, error) {
	c := cfapi.New(token)
	if err := c.Verify(ctx); err != nil {
		return nil, nil, err
	}
	accounts, err := c.ListAccounts(ctx)
	if err != nil {
		return nil, nil, err
	}
	zones, err := c.ListZones(ctx)
	if err != nil {
		return nil, nil, err
	}
	return accounts, zones, nil
}

type Session struct {
	Config  config.File
	Command string
	SSHFile string
	KeyPath string
}

func (o *Orchestrator) Start(ctx context.Context, cfg config.File, sshPassword string) (Session, error) {
	o.mu.Lock()
	if o.helper != nil && o.helper.Process != nil && supervisor.Alive(o.helper.Process.Pid) {
		o.mu.Unlock()
		return Session{}, errors.New("已经在运行")
	}
	o.mu.Unlock()

	if err := validate(cfg); err != nil {
		return Session{}, err
	}
	if err := auth.VerifyCurrentUser(sshPassword); err != nil {
		return Session{}, err
	}
	cfg.PasswordAuth = true
	o.Log("已核对当前用户的 SSH 登录密码。客户端要用同一个密码。密码不会写入配置文件。")
	host := config.Hostname(cfg.Subdomain, cfg.ZoneName)
	userName := clientcfg.CurrentUser()
	allowUser := ""
	if userRE.MatchString(userName) {
		allowUser = userName
	} else {
		o.Log("当前用户名无法写入 AllowUsers，不限制 SSH 用户名: " + userName)
	}

	base, err := config.Dir()
	if err != nil {
		return Session{}, err
	}
	runDir := filepath.Join(base, "run")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return Session{}, err
	}
	o.mu.Lock()
	o.runDir = runDir
	o.mu.Unlock()

	o.Log("准备 cloudflared")
	bin, err := cloudflared.Ensure(ctx, filepath.Join(base, "bin"), o.Log)
	if err != nil {
		return Session{}, err
	}
	keyPath, err := clientcfg.EnsureKey(o.Log)
	if err != nil {
		if !cfg.PasswordAuth {
			return Session{}, err
		}
		o.Log("没有生成本机密钥，登录时使用系统密码: " + err.Error())
		keyPath = ""
	}

	port, err := supervisor.PickPort(cfg.Port)
	if err != nil {
		return Session{}, err
	}
	if port != cfg.Port {
		o.Log(fmt.Sprintf("端口 %d 已被占用，改用 %d", cfg.Port, port))
	}
	cfg.Port = port

	c := cfapi.New(cfg.APIToken)
	o.Log("创建或复用 Cloudflare 隧道")
	tun, err := c.EnsureRemoteTunnel(ctx, cfg.AccountID, cfg.TunnelName)
	if err != nil {
		return Session{}, err
	}
	cfg.TunnelID = tun.ID
	o.Log("隧道 " + tun.Name + " " + tun.ID)

	if cfg.Hostname != "" && !strings.EqualFold(cfg.Hostname, host) {
		o.Log("删除旧的 DNS 记录 " + cfg.Hostname)
		if err := c.DeleteHostname(ctx, cfg.ZoneID, cfg.Hostname); err != nil {
			o.Log("删除旧 DNS 失败，继续使用新主机名: " + err.Error())
		}
	}
	service := fmt.Sprintf("ssh://127.0.0.1:%d", port)
	o.Log("发布 " + host + " → " + service)
	if err := c.PutSSHIngress(ctx, cfg.AccountID, tun.ID, host, service); err != nil {
		return Session{}, err
	}
	if err := c.UpsertTunnelCNAME(ctx, cfg.ZoneID, host, tun.ID); err != nil {
		return Session{}, err
	}
	o.Log("配置 Cloudflare Access 服务令牌。客户端不用收验证码。")
	cred, err := c.EnsureAccessSSH(ctx, cfg.AccountID, host, cfapi.AccessCredential{
		ID:       cfg.ServiceTokenID,
		ClientID: cfg.ServiceTokenClientID,
		Secret:   cfg.ServiceTokenSecret,
	})
	if err != nil {
		return Session{}, err
	}
	cfg.ServiceTokenID = cred.ID
	cfg.ServiceTokenClientID = cred.ClientID
	cfg.ServiceTokenSecret = cred.Secret
	cfg.Hostname = host
	if err := config.Save(cfg); err != nil {
		return Session{}, err
	}

	req := supervisor.Request{
		ParentPID:    os.Getpid(),
		Port:         port,
		Listen:       config.ListenAddr(cfg.LocalhostOnly),
		AllowUser:    allowUser,
		PasswordAuth: cfg.PasswordAuth,
		PermitRoot:   userName == "root",
		Cloudflared:  bin,
		TunnelToken:  tun.Token,
		Hostname:     host,
	}
	if err := supervisor.WriteRequest(runDir, req); err != nil {
		return Session{}, err
	}
	_ = os.Remove(supervisor.StatusPath(runDir))
	_ = os.Remove(supervisor.ShutdownPath(runDir))
	_ = os.Remove(supervisor.LogPath(runDir))

	o.Log("请求管理员权限并启动 SSH 与隧道。系统可能会弹出密码或 UAC 窗口。")
	helper, err := supervisor.StartHelper(runDir)
	if err != nil {
		return Session{}, err
	}
	o.mu.Lock()
	o.helper = helper
	o.mu.Unlock()

	deadline := time.Now().Add(10 * time.Minute)
	for {
		if ctx.Err() != nil {
			o.Stop()
			return Session{}, ErrStopped
		}
		if time.Now().After(deadline) {
			o.Stop()
			return Session{}, errors.New("等待启动超时")
		}
		st, err := supervisor.ReadStatus(runDir)
		if err == nil && st.State == "running" {
			cmd := clientcfg.Command(userName, host, cfg.ServiceTokenClientID, cfg.ServiceTokenSecret)
			file := clientcfg.SSHConfig(userName, host, keyPath, cfg.ServiceTokenClientID, cfg.ServiceTokenSecret)
			o.Log("可以使用: ssh " + userName + "@" + host)
			o.Log("客户端也要安装 cloudflared。连接时只输入系统密码，不用邮箱验证码。")
			return Session{Config: cfg, Command: cmd, SSHFile: file, KeyPath: keyPath}, nil
		}
		if err == nil && st.State == "stopped" {
			return Session{}, ErrStopped
		}
		if err == nil && st.State == "error" {
			o.Stop()
			detail := st.Detail
			if extra := tail(supervisor.LogPath(runDir), 20); extra != "" {
				detail += "\n" + extra
			}
			return Session{}, errors.New(detail)
		}
		if helper.Process == nil || !supervisor.Alive(helper.Process.Pid) {
			detail := "启动进程退出了"
			if extra := tail(filepath.Join(runDir, "launch.log"), 30); extra != "" {
				detail += "\n" + extra
			}
			if extra := tail(supervisor.LogPath(runDir), 20); extra != "" {
				detail += "\n" + extra
			}
			return Session{}, errors.New(detail)
		}
		select {
		case <-ctx.Done():
			o.Stop()
			return Session{}, ErrStopped
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func (o *Orchestrator) Stop() {
	o.mu.Lock()
	dir := o.runDir
	helper := o.helper
	o.mu.Unlock()
	if dir == "" {
		return
	}
	wasLive := helper != nil && helper.Process != nil && supervisor.Alive(helper.Process.Pid)
	_ = os.WriteFile(supervisor.ShutdownPath(dir), []byte("stop\n"), 0o644)
	if !wasLive {
		return
	}
	o.Log("正在停止隧道和 SSH")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if helper.Process == nil || !supervisor.Alive(helper.Process.Pid) {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if helper.Process != nil && supervisor.Alive(helper.Process.Pid) {
		o.Log("停止超时。如果 SSH 还在，请再点一次停止，或结束 cfssh helper 进程。")
		return
	}
	o.mu.Lock()
	o.helper = nil
	o.mu.Unlock()
	o.Log("Cloudflare Tunnel 和 SSH 已停止。云端隧道和 DNS 记录仍保留，下次启动会复用。")
}

func validate(cfg config.File) error {
	if strings.TrimSpace(cfg.APIToken) == "" {
		return errors.New("先填写 Cloudflare API 令牌")
	}
	if cfg.AccountID == "" {
		return errors.New("先验证令牌并选择账户")
	}
	if cfg.ZoneID == "" || cfg.ZoneName == "" {
		return errors.New("选择一个托管在 Cloudflare 上的域名")
	}
	if !config.ValidSubdomain(cfg.Subdomain) {
		return errors.New("主机名前缀只能用小写字母、数字和连字符")
	}
	if cfg.Port < 1 || cfg.Port > 65535 {
		return errors.New("端口要在 1 到 65535 之间")
	}
	return nil
}

func tail(path string, n int) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
