package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"cfssh/internal/cfapi"
	"cfssh/internal/clientcfg"
	"cfssh/internal/config"
	"cfssh/internal/orchestrator"
	"cfssh/internal/supervisor"
)

type accountOpt struct {
	ID   string
	Name string
}

type zoneOpt struct {
	ID        string
	Name      string
	AccountID string
}

// Run shows the window. Closing it stops the tunnel and the SSH server.
func Run(orch *orchestrator.Orchestrator) {
	a := app.NewWithID("dev.cfssh.app")
	a.Settings().SetTheme(newCJKTheme())
	w := a.NewWindow("CF SSH")
	w.Resize(fyne.NewSize(880, 760))

	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}

	status := canvas.NewText("未运行", color.NRGBA{R: 140, G: 70, B: 40, A: 255})
	status.TextSize = 18
	status.TextStyle = fyne.TextStyle{Bold: true}

	intro := widget.NewLabel("主要给 Windows 用，同样能在 Fedora、Ubuntu 和 macOS 上编译。本机启动 SSH，只用 Cloudflare Tunnel 转到公网。关掉窗口就停掉隧道和 SSH。需要一个已经托管在 Cloudflare 的域名。")
	intro.Wrapping = fyne.TextWrapWord

	token := widget.NewPasswordEntry()
	if cfg.APIToken != "" {
		token.SetPlaceHolder("已保存在本机，留空则继续使用")
	} else {
		token.SetPlaceHolder("粘贴 Cloudflare API 令牌")
	}

	accountSel := widget.NewSelect([]string{}, nil)
	zoneSel := widget.NewSelect([]string{}, nil)
	var accounts []accountOpt
	var zones []zoneOpt

	sub := widget.NewEntry()
	sub.SetText(cfg.Subdomain)
	sub.SetPlaceHolder("ssh")
	hostPreview := widget.NewLabel(hostText(cfg.Subdomain, cfg.ZoneName))

	port := widget.NewEntry()
	port.SetText(strconv.Itoa(cfg.Port))
	localhost := widget.NewCheck("只监听 127.0.0.1（推荐，局域网也进不来）", nil)
	localhost.SetChecked(cfg.LocalhostOnly)
	sshPass := widget.NewPasswordEntry()
	sshPass.SetPlaceHolder("当前用户的登录密码")
	sshPass2 := widget.NewPasswordEntry()
	sshPass2.SetPlaceHolder("再输入一次")
	passHint := widget.NewLabel(passwordHint())
	passHint.Wrapping = fyne.TextWrapWord

	command := widget.NewMultiLineEntry()
	command.SetText("启动后这里会显示客户端命令。对方电脑也要安装 cloudflared。")
	command.Wrapping = fyne.TextWrapWord

	logBox := widget.NewMultiLineEntry()
	logBox.Wrapping = fyne.TextWrapWord
	logBox.Disable()

	var busy bool
	var runCancel context.CancelFunc
	startBtn := widget.NewButton("启动", nil)
	stopBtn := widget.NewButton("停止", nil)
	stopBtn.Disable()

	refreshHost := func() {
		name := selectedZoneName(zoneSel, zones)
		hostPreview.SetText(hostText(sub.Text, name))
	}
	sub.OnChanged = func(string) { refreshHost() }
	zoneSel.OnChanged = func(string) { refreshHost() }

	applyAccounts := func(list []cfapi.Account, zlist []cfapi.Zone) {
		accounts = accounts[:0]
		seen := map[string]bool{}
		for _, item := range list {
			if item.ID == "" || seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			name := item.Name
			if name == "" {
				name = "当前账户"
			}
			accounts = append(accounts, accountOpt{ID: item.ID, Name: name})
		}
		zones = zones[:0]
		for _, item := range zlist {
			zones = append(zones, zoneOpt{ID: item.ID, Name: item.Name, AccountID: item.AccountID})
			if item.AccountID == "" || seen[item.AccountID] {
				continue
			}
			seen[item.AccountID] = true
			accounts = append(accounts, accountOpt{ID: item.AccountID, Name: "当前账户"})
		}
		if len(list) == 0 && len(accounts) > 1 {
			for i := range accounts {
				short := accounts[i].ID
				if len(short) > 6 {
					short = short[:6]
				}
				accounts[i].Name = "账户 " + short
			}
		}
		labels := make([]string, 0, len(accounts))
		for _, item := range accounts {
			labels = append(labels, item.Name)
		}
		accountSel.Options = labels
		accountSel.Refresh()
		picked := false
		if cfg.AccountID != "" {
			for _, item := range accounts {
				if item.ID == cfg.AccountID {
					accountSel.SetSelected(item.Name)
					picked = true
					break
				}
			}
		}
		if !picked && len(labels) == 1 {
			accountSel.SetSelected(labels[0])
		}
		refillZones(accountSel, zoneSel, accounts, zones, cfg.ZoneID)
		refreshHost()
	}

	accountSel.OnChanged = func(string) {
		refillZones(accountSel, zoneSel, accounts, zones, "")
		refreshHost()
	}
	if cfg.AccountID != "" || cfg.ZoneID != "" {
		seed := []cfapi.Account{}
		if cfg.AccountID != "" {
			seed = append(seed, cfapi.Account{ID: cfg.AccountID, Name: "当前账户"})
		}
		var seedZones []cfapi.Zone
		if cfg.ZoneID != "" && cfg.ZoneName != "" {
			seedZones = []cfapi.Zone{{ID: cfg.ZoneID, Name: cfg.ZoneName, AccountID: cfg.AccountID}}
		}
		applyAccounts(seed, seedZones)
	}

	setStatus := func(text string, c color.NRGBA) {
		status.Text = text
		status.Color = c
		status.Refresh()
	}
	refreshLog := func() {
		text := orch.Text()
		if dir := orch.RunDir(); dir != "" {
			if extra := readTail(supervisor.LogPath(dir), 80); extra != "" {
				if text != "" {
					text += "\n"
				}
				text += extra
			}
		}
		if logBox.Text != text {
			logBox.SetText(text)
		}
	}

	orch.OnChange(func() {
		fyne.Do(refreshLog)
	})

	collect := func() (config.File, string, error) {
		next := cfg
		next.APIToken = strings.TrimSpace(token.Text)
		if next.APIToken == "" {
			next.APIToken = cfg.APIToken
		}
		next.Subdomain = strings.TrimSpace(sub.Text)
		next.LocalhostOnly = localhost.Checked
		next.PasswordAuth = true
		sshPassword := sshPass.Text
		if strings.TrimSpace(sshPassword) == "" {
			return next, "", errors.New("请填写当前用户的 SSH 登录密码")
		}
		if sshPassword != sshPass2.Text {
			return next, "", errors.New("两次输入的 SSH 密码不一致")
		}
		p, err := strconv.Atoi(strings.TrimSpace(port.Text))
		if err != nil {
			return next, "", errors.New("端口要是数字")
		}
		next.Port = p
		acc, ok := selectedAccount(accountSel, accounts)
		if !ok {
			return next, "", errors.New("先验证令牌并选择账户")
		}
		next.AccountID = acc.ID
		zone, ok := selectedZone(zoneSel, zones, acc.ID)
		if !ok {
			return next, "", errors.New("选择域名")
		}
		next.ZoneID = zone.ID
		next.ZoneName = zone.Name
		return next, sshPassword, nil
	}

	var verifyBtn *widget.Button
	verifyBtn = widget.NewButton("验证令牌并拉取域名", func() {
		if busy {
			return
		}
		tok := strings.TrimSpace(token.Text)
		if tok == "" {
			tok = cfg.APIToken
		}
		if tok == "" {
			orch.Log("先粘贴 API 令牌")
			return
		}
		busy = true
		verifyBtn.Disable()
		orch.Log("正在验证令牌")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			acc, zs, err := orchestrator.TestToken(ctx, tok)
			fyne.Do(func() {
				busy = false
				verifyBtn.Enable()
				if err != nil {
					orch.Log("令牌验证失败: " + err.Error())
					setStatus("令牌无效", color.NRGBA{R: 170, G: 40, B: 40, A: 255})
					return
				}
				cfg.APIToken = tok
				applyAccounts(acc, zs)
				if err := config.Save(cfg); err != nil {
					orch.Log("保存配置失败: " + err.Error())
				}
				if len(acc) == 0 && len(zs) > 0 {
					orch.Log(fmt.Sprintf("令牌可用，域名 %d 个。这把令牌列不出账户名称，已按域名选中账户。", len(zs)))
				} else {
					orch.Log(fmt.Sprintf("令牌可用，%d 个账户，%d 个域名", len(acc), len(zs)))
				}
				setStatus("令牌已验证", color.NRGBA{R: 20, G: 120, B: 70, A: 255})
				refreshLog()
			})
		}()
	})

	startBtn.OnTapped = func() {
		if busy {
			return
		}
		next, sshPassword, err := collect()
		if err != nil {
			orch.Log(err.Error())
			return
		}
		cfg = next
		if err := config.Save(cfg); err != nil {
			orch.Log("保存配置失败: " + err.Error())
		}
		busy = true
		startBtn.Disable()
		stopBtn.Enable()
		setStatus("正在启动…", color.NRGBA{R: 40, G: 90, B: 160, A: 255})
		ctx, cancel := context.WithCancel(context.Background())
		runCancel = cancel
		go func() {
			session, err := orch.Start(ctx, cfg, sshPassword)
			fyne.Do(func() {
				busy = false
				runCancel = nil
				refreshLog()
				if err != nil {
					if errors.Is(err, orchestrator.ErrStopped) {
						setStatus("已停止", color.NRGBA{R: 140, G: 70, B: 40, A: 255})
					} else {
						setStatus("启动失败", color.NRGBA{R: 170, G: 40, B: 40, A: 255})
						orch.Log(err.Error())
					}
					startBtn.Enable()
					stopBtn.Disable()
					return
				}
				cfg = session.Config
				port.SetText(strconv.Itoa(cfg.Port))
				var text strings.Builder
				text.WriteString(session.Command)
				text.WriteString("\n\n")
				text.WriteString(session.SSHFile)
				fmt.Fprintf(&text, "\n登录用户: %s\nSSH 密码: 启动时填写的当前用户密码\n", clientcfg.CurrentUser())
				if session.KeyPath != "" {
					text.WriteString("私钥在这台电脑上: ")
					text.WriteString(session.KeyPath)
					text.WriteString("\n客户端也可以改用这把密钥。\n")
				}
				command.SetText(text.String())
				setStatus("运行中  "+cfg.Hostname, color.NRGBA{R: 20, G: 120, B: 70, A: 255})
				stopBtn.Enable()
			})
		}()
	}

	stopBtn.OnTapped = func() {
		if runCancel != nil {
			runCancel()
		}
		go func() {
			orch.Stop()
			fyne.Do(func() {
				busy = false
				startBtn.Enable()
				stopBtn.Disable()
				setStatus("已停止", color.NRGBA{R: 140, G: 70, B: 40, A: 255})
				refreshLog()
			})
		}()
	}

	copyCmd := widget.NewButton("复制连接命令", func() {
		line := firstLine(command.Text)
		w.Clipboard().SetContent(line)
		orch.Log("已复制连接命令")
	})
	copyCfg := widget.NewButton("复制 SSH 配置", func() {
		w.Clipboard().SetContent(sshConfigBlock(command.Text))
		orch.Log("已复制 SSH 配置")
	})

	help := widget.NewLabel("令牌权限：账户 Cloudflare Tunnel 编辑；账户 Access 应用和策略编辑；账户 Access 服务令牌编辑；区域 DNS 编辑。区域范围选你的域名。客户端把上面的配置放进 ~/.ssh/config 后直接 ssh。连接时只输入系统密码，不用邮箱验证码。")
	help.Wrapping = fyne.TextWrapWord

	tokenPage, _ := parseURL("https://dash.cloudflare.com/profile/api-tokens")
	link := widget.NewHyperlink("打开 API 令牌页面", tokenPage)

	form := container.NewVBox(
		intro,
		container.NewBorder(nil, nil, widget.NewLabel("API 令牌"), verifyBtn, token),
		container.NewGridWithColumns(2,
			container.NewBorder(nil, nil, widget.NewLabel("账户"), nil, accountSel),
			container.NewBorder(nil, nil, widget.NewLabel("域名"), nil, zoneSel),
		),
		container.NewBorder(nil, nil, widget.NewLabel("主机名"), nil, sub),
		hostPreview,
		container.NewGridWithColumns(2,
			container.NewBorder(nil, nil, widget.NewLabel("SSH 端口"), nil, port),
			localhost,
		),
		widget.NewLabel("当前用户的 SSH 密码"),
		container.NewGridWithColumns(2, sshPass, sshPass2),
		passHint,
		link,
		help,
		container.NewHBox(startBtn, stopBtn, layout.NewSpacer(), copyCmd, copyCfg),
		widget.NewLabel("客户端"),
		command,
	)

	split := container.NewVSplit(
		container.NewVScroll(form),
		container.NewBorder(widget.NewLabel("日志"), nil, nil, nil, logBox),
	)
	split.Offset = 0.68

	header := container.NewVBox(
		container.NewHBox(widget.NewLabel("CF SSH"), layout.NewSpacer(), status),
		widget.NewLabel(fmt.Sprintf("%s/%s  ·  当前用户 %s", runtime.GOOS, runtime.GOARCH, clientcfg.CurrentUser())),
	)
	w.SetContent(container.NewBorder(container.NewPadded(header), nil, nil, nil, split))

	w.SetCloseIntercept(func() {
		if runCancel != nil {
			runCancel()
		}
		setStatus("正在停止…", color.NRGBA{R: 140, G: 70, B: 40, A: 255})
		done := make(chan struct{})
		go func() {
			orch.Stop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(25 * time.Second):
		}
		w.Close()
	})

	w.Show()
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			fyne.Do(func() {
				refreshLog()
				if orch.Running() {
					setStatus("运行中  "+cfg.Hostname, color.NRGBA{R: 20, G: 120, B: 70, A: 255})
				}
			})
		}
	}()
	if cfg.APIToken != "" && cfg.AccountID != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			acc, zs, err := orchestrator.TestToken(ctx, cfg.APIToken)
			if err != nil {
				fyne.Do(func() { orch.Log("启动时读取域名失败: " + err.Error()) })
				return
			}
			fyne.Do(func() { applyAccounts(acc, zs) })
		}()
	}
	a.Run()
}

func passwordHint() string {
	if runtime.GOOS == "windows" {
		return "填写当前 Windows 用户的登录密码。别人 SSH 进来时要输入这个密码。程序只在启动前向系统核对，不写入配置文件，也不改你的 Windows 密码。"
	}
	return "填写当前系统用户的登录密码。别人 SSH 进来时要输入这个密码。程序只在启动前核对，不写入配置文件。"
}

func hostText(sub, zone string) string {
	sub = strings.TrimSpace(sub)
	zone = strings.TrimSpace(zone)
	if sub == "" || zone == "" {
		return "公网主机名会显示在这里"
	}
	return "公网主机名：" + sub + "." + zone
}

func refillZones(accountSel, zoneSel *widget.Select, accounts []accountOpt, zones []zoneOpt, preferID string) {
	acc, ok := selectedAccount(accountSel, accounts)
	labels := []string{}
	if ok {
		for _, zone := range zones {
			if zone.AccountID == "" || zone.AccountID == acc.ID {
				labels = append(labels, zone.Name)
			}
		}
	}
	zoneSel.Options = labels
	stillThere := false
	for _, label := range labels {
		if label == zoneSel.Selected {
			stillThere = true
			break
		}
	}
	if !stillThere {
		zoneSel.ClearSelected()
	}
	zoneSel.Refresh()
	if preferID != "" {
		for _, zone := range zones {
			if zone.ID == preferID {
				zoneSel.SetSelected(zone.Name)
				return
			}
		}
	}
	if len(labels) == 1 {
		zoneSel.SetSelected(labels[0])
	}
}

func selectedAccount(sel *widget.Select, accounts []accountOpt) (accountOpt, bool) {
	for _, item := range accounts {
		if item.Name == sel.Selected {
			return item, true
		}
	}
	return accountOpt{}, false
}

func selectedZone(sel *widget.Select, zones []zoneOpt, accountID string) (zoneOpt, bool) {
	for _, item := range zones {
		if item.Name == sel.Selected && (item.AccountID == "" || item.AccountID == accountID) {
			return item, true
		}
	}
	return zoneOpt{}, false
}

func selectedZoneName(sel *widget.Select, zones []zoneOpt) string {
	for _, item := range zones {
		if item.Name == sel.Selected {
			return item.Name
		}
	}
	return ""
}

func firstLine(text string) string {
	text = strings.TrimSpace(text)
	if i := strings.IndexByte(text, '\n'); i >= 0 {
		return text[:i]
	}
	return text
}

func sshConfigBlock(text string) string {
	if i := strings.Index(text, "Host "); i >= 0 {
		rest := text[i:]
		if j := strings.Index(rest, "\n私钥"); j >= 0 {
			return strings.TrimSpace(rest[:j])
		}
		return strings.TrimSpace(rest)
	}
	return strings.TrimSpace(text)
}

func readTail(path string, n int) string {
	data, err := readFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
