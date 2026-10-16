//go:build !windows

package ui

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"cfssh/internal/auth"
	"cfssh/internal/clientcfg"
	"cfssh/internal/orchestrator"
	"cfssh/internal/provision"
)

// runCustomer is the shipped-to-customer window. Everything about Cloudflare was
// decided ahead of time and lives in the sidecar bundle, so the only thing the
// customer touches is their own login password.
func runCustomer(orch *orchestrator.Orchestrator, b provision.File) {
	a := app.NewWithID("dev.cfssh.app")
	a.Settings().SetTheme(newCJKTheme())
	w := a.NewWindow("CF SSH")
	w.Resize(fyne.NewSize(760, 760))

	status := canvas.NewText("未运行", color.NRGBA{R: 140, G: 70, B: 40, A: 255})
	status.TextSize = 18
	status.TextStyle = fyne.TextStyle{Bold: true}

	intro := widget.NewLabel("点启动后，这台电脑的 SSH 会通过 Cloudflare 安全地暴露到公网。关掉窗口就会停掉。\n公网地址：" + b.Hostname)
	intro.Wrapping = fyne.TextWrapBreak

	sshPass := widget.NewPasswordEntry()
	sshPass.SetPlaceHolder("当前用户的登录密码")
	sshPass2 := widget.NewPasswordEntry()
	sshPass2.SetPlaceHolder("再输入一次")
	passHint := widget.NewLabel(passwordHint())
	passHint.Wrapping = fyne.TextWrapBreak
	passLabel := widget.NewLabel("当前用户的登录密码")

	// Users who do not know their password can give the account a new one,
	// if they are an administrator of a local account. Shown only when allowed.
	user := clientcfg.CurrentUser()
	setNew := widget.NewCheck("不知道当前密码？给 "+user+" 设置一个新密码", func(on bool) {
		if on {
			passLabel.SetText("给 " + user + " 设置的新密码")
			sshPass.SetPlaceHolder("新密码")
			passHint.SetText(newPasswordHint(user))
		} else {
			passLabel.SetText("当前用户的登录密码")
			sshPass.SetPlaceHolder("当前用户的登录密码")
			passHint.SetText(passwordHint())
		}
	})
	setNew.Hide()
	go func() {
		if ok, _ := auth.SetPasswordSupport(); ok {
			fyne.Do(setNew.Show)
		}
	}()

	command := widget.NewMultiLineEntry()
	command.SetText("启动后这里会显示连接命令，发给要登录的人。对方电脑也要安装 cloudflared。")
	command.Wrapping = fyne.TextWrapWord
	command.SetMinRowsVisible(4)

	logs := newLogView()

	var busy bool
	var runCancel context.CancelFunc
	startBtn := widget.NewButton("启动", nil)
	stopBtn := widget.NewButton("停止", nil)
	stopBtn.Disable()

	setStatus := func(text string, c color.NRGBA) {
		status.Text = text
		status.Color = c
		status.Refresh()
	}
	refreshLog := func() { logs.Set(customerLogText(orch)) }
	orch.OnChange(func() { fyne.Do(refreshLog) })

	var launch func(pw string, newPassword bool)
	startBtn.OnTapped = func() {
		if busy {
			return
		}
		pw := sshPass.Text
		if strings.TrimSpace(pw) == "" {
			if setNew.Checked {
				orch.Log("请填写新密码")
			} else {
				orch.Log("请填写当前用户的登录密码")
			}
			return
		}
		if pw != sshPass2.Text {
			orch.Log("两次输入的密码不一致")
			return
		}
		if !setNew.Checked {
			launch(pw, false)
			return
		}
		dialog.ShowConfirm("修改 Windows 登录密码",
			"启动时会把 Windows 用户 "+user+" 的登录密码改成刚才输入的新密码。\n以后登录这台电脑（包括远程桌面）都要用新密码。\n\n确定继续吗？",
			func(yes bool) {
				if yes {
					launch(pw, true)
				}
			}, w)
	}
	launch = func(pw string, newPassword bool) {
		busy = true
		startBtn.Disable()
		stopBtn.Enable()
		setStatus("正在启动…", color.NRGBA{R: 40, G: 90, B: 160, A: 255})
		ctx, cancel := context.WithCancel(context.Background())
		runCancel = cancel
		go func() {
			session, err := orch.Launch(ctx, b, pw, newPassword)
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
				var text strings.Builder
				text.WriteString(session.Command)
				text.WriteString("\n\n")
				text.WriteString(session.SSHFile)
				passNote := "刚才填写的当前用户密码"
				if newPassword {
					passNote = "刚才设置的新密码（也是这台电脑新的登录密码）"
					// The account now has this password; later starts just check it.
					setNew.SetChecked(false)
				}
				fmt.Fprintf(&text, "\n登录用户: %s\nSSH 密码: %s\n", user, passNote)
				command.SetText(text.String())
				setStatus("运行中  "+b.Hostname, color.NRGBA{R: 20, G: 120, B: 70, A: 255})
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
		w.Clipboard().SetContent(firstLine(command.Text))
		orch.Log("已复制连接命令")
	})
	copyCfg := widget.NewButton("复制 SSH 配置", func() {
		w.Clipboard().SetContent(sshConfigBlock(command.Text))
		orch.Log("已复制 SSH 配置")
	})

	form := container.NewVBox(
		intro,
		passLabel,
		container.NewGridWithColumns(2, sshPass, sshPass2),
		setNew,
		passHint,
		container.NewHBox(startBtn, stopBtn, layout.NewSpacer(), copyCmd, copyCfg),
		widget.NewLabel("连接命令"),
		command,
	)

	// The form keeps its full height (a split at a fixed ratio cut the
	// connection-command box in half); the log takes whatever is left.
	body := container.NewBorder(form, nil, nil, nil,
		container.NewBorder(widget.NewLabel("日志"), nil, nil, nil, logs.scroll))

	header := container.NewVBox(
		container.NewHBox(widget.NewLabel("CF SSH"), layout.NewSpacer(), status),
		widget.NewLabel(fmt.Sprintf("%s/%s  ·  当前用户 %s", runtime.GOOS, runtime.GOARCH, clientcfg.CurrentUser())),
	)
	w.SetContent(container.NewBorder(container.NewPadded(header), nil, nil, nil, body))

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
	wasRunning := false
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			fyne.Do(func() {
				refreshLog()
				if orch.Running() {
					wasRunning = true
					setStatus("运行中  "+b.Hostname, color.NRGBA{R: 20, G: 120, B: 70, A: 255})
					return
				}
				// The tunnel or sshd died on its own: say so instead of
				// leaving "running" on screen, and let the user start again.
				if wasRunning && !busy {
					wasRunning = false
					if st, ok := orch.LastStatus(); ok && st.State == "error" {
						setStatus("已断开", color.NRGBA{R: 170, G: 40, B: 40, A: 255})
						orch.Log(st.Detail)
					} else {
						setStatus("已停止", color.NRGBA{R: 140, G: 70, B: 40, A: 255})
					}
					startBtn.Enable()
					stopBtn.Disable()
				}
			})
		}
	}()
	a.Run()
}
