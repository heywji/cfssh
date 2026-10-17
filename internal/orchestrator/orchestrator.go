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
	"cfssh/internal/provision"
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

// Reconnecting reports whether the helper is running but has lost the tunnel
// and is bringing it back.
func (o *Orchestrator) Reconnecting() bool {
	st, ok := o.LastStatus()
	return ok && st.State == "running" && st.Tunnel == "reconnecting"
}

// LastStatus returns the helper's most recent status, if a run has started.
func (o *Orchestrator) LastStatus() (supervisor.Status, bool) {
	dir := o.RunDir()
	if dir == "" {
		return supervisor.Status{}, false
	}
	st, err := supervisor.ReadStatus(dir)
	return st, err == nil
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

// Provision performs the operator-side Cloudflare setup: it creates or reuses the
// named tunnel, publishes the SSH ingress, upserts the DNS record and ensures the
// Access application plus its service token. It returns the sidecar bundle a
// customer machine needs — notably the tunnel token, never the API token. It makes
// no local changes (no sshd, no cloudflared process), so it is safe to run from an
// operator workstation that is not the target machine.
func Provision(ctx context.Context, cfg config.File) (provision.File, error) {
	if err := validate(cfg); err != nil {
		return provision.File{}, err
	}
	host := config.Hostname(cfg.Subdomain, cfg.ZoneName)
	c := cfapi.New(cfg.APIToken)

	tun, err := c.EnsureRemoteTunnel(ctx, cfg.AccountID, cfg.TunnelName)
	if err != nil {
		return provision.File{}, err
	}
	service := fmt.Sprintf("ssh://127.0.0.1:%d", cfg.Port)
	if err := c.PutSSHIngress(ctx, cfg.AccountID, tun.ID, host, service); err != nil {
		return provision.File{}, err
	}
	if err := c.UpsertTunnelCNAME(ctx, cfg.ZoneID, host, tun.ID); err != nil {
		return provision.File{}, err
	}
	cred, err := c.EnsureAccessSSH(ctx, cfg.AccountID, host, cfapi.AccessCredential{
		ID:       cfg.ServiceTokenID,
		ClientID: cfg.ServiceTokenClientID,
		Secret:   cfg.ServiceTokenSecret,
	})
	if err != nil {
		return provision.File{}, err
	}
	return provision.File{
		Hostname:             host,
		Port:                 cfg.Port,
		LocalhostOnly:        cfg.LocalhostOnly,
		TunnelToken:          tun.Token,
		TunnelID:             tun.ID,
		TunnelName:           tun.Name,
		ServiceTokenClientID: cred.ClientID,
		ServiceTokenSecret:   cred.Secret,
		ZoneName:             cfg.ZoneName,
	}, nil
}

// Launch brings up SSH and the Cloudflare tunnel on this machine using a
// pre-provisioned bundle. It touches Cloudflare not at all: the tunnel token in
// the bundle is all cloudflared needs. The only secret the caller supplies is the
// current user's login password, which is checked against the OS and never stored.
// When setNew is true the account is given sshPassword as its new login
// password (Windows administrators only) instead of checking the current one.
func (o *Orchestrator) Launch(ctx context.Context, b provision.File, sshPassword string, setNew bool) (Session, error) {
	o.mu.Lock()
	if o.helper != nil && o.helper.Process != nil && supervisor.Alive(o.helper.Process.Pid) {
		o.mu.Unlock()
		return Session{}, errors.New("已经在运行")
	}
	o.mu.Unlock()

	if err := b.Validate(); err != nil {
		return Session{}, err
	}
	userName := clientcfg.CurrentUser()
	sealed := ""
	if setNew {
		if ok, why := auth.SetPasswordSupport(); !ok {
			return Session{}, errors.New(why)
		}
		s, err := auth.SealPassword(sshPassword)
		if err != nil {
			return Session{}, err
		}
		sealed = s
		o.Log("启动时会把 Windows 用户 " + userName + " 的登录密码改成新密码。客户端 SSH 时用这个新密码。")
	} else {
		if err := auth.VerifyCurrentUser(sshPassword); err != nil {
			return Session{}, err
		}
		o.Log("已核对当前用户的 SSH 登录密码。客户端要用同一个密码。密码不会写入配置文件。")
	}

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
		o.Log("没有生成本机密钥，登录时使用系统密码: " + err.Error())
		keyPath = ""
	}

	// The ingress service URL baked into the tunnel points at this exact port, so
	// the customer runtime must bind it; it cannot silently fall back to another
	// port the way the operator all-in-one flow could, because it holds no API
	// token to update the ingress.
	free, err := supervisor.PickPort(b.Port)
	if err != nil {
		return Session{}, err
	}
	if free != b.Port {
		return Session{}, fmt.Errorf("端口 %d 已被占用，无法启动。该端口已写入云端隧道配置，换端口需要重新预配。", b.Port)
	}

	req := supervisor.Request{
		ParentPID:    os.Getpid(),
		Port:         b.Port,
		Listen:       config.ListenAddr(b.LocalhostOnly),
		AllowUser:    allowUser,
		PasswordAuth: true,
		PermitRoot:   userName == "root",
		Cloudflared:  bin,
		TunnelToken:  b.TunnelToken,
		Hostname:     b.Hostname,
		UserName:     userName,
		NewPassword:  sealed,
	}
	cfg := config.File{Hostname: b.Hostname, Port: b.Port, LocalhostOnly: b.LocalhostOnly, PasswordAuth: true}
	return o.runHelper(ctx, runDir, req, cfg, userName, b.Hostname, keyPath, b.ServiceTokenClientID, b.ServiceTokenSecret)
}

// Start is the operator all-in-one path (used by the advanced GUI when no sidecar
// is present): it provisions Cloudflare from cfg and then launches locally.
func (o *Orchestrator) Start(ctx context.Context, cfg config.File, sshPassword string) (Session, error) {
	b, err := Provision(ctx, cfg)
	if err != nil {
		return Session{}, err
	}
	o.Log("隧道 " + b.TunnelName + " " + b.TunnelID)
	o.Log("发布 " + b.Hostname + fmt.Sprintf(" → ssh://127.0.0.1:%d", b.Port))
	o.Log("Cloudflare Access 服务令牌已就绪，客户端不用收验证码。")
	// Persist the operator config so the advanced GUI remembers account/zone/token.
	cfg.TunnelID = b.TunnelID
	cfg.Hostname = b.Hostname
	cfg.ServiceTokenClientID = b.ServiceTokenClientID
	cfg.ServiceTokenSecret = b.ServiceTokenSecret
	cfg.PasswordAuth = true
	if err := config.Save(cfg); err != nil {
		o.Log("保存配置失败: " + err.Error())
	}
	return o.Launch(ctx, b, sshPassword, false)
}

// runHelper writes the request, starts the privileged helper and waits for it to
// report running, erroring out or dying. Shared by every launch path.
func (o *Orchestrator) runHelper(ctx context.Context, runDir string, req supervisor.Request, cfg config.File, userName, host, keyPath, clientID, secret string) (Session, error) {
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
			cmd := clientcfg.Command(userName, host, clientID, secret)
			file := clientcfg.SSHConfig(userName, host, keyPath, clientID, secret)
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
