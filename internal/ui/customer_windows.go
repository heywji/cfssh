//go:build windows

package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"cfssh/internal/auth"
	"cfssh/internal/clientcfg"
	"cfssh/internal/orchestrator"
	"cfssh/internal/provision"
	"cfssh/internal/supervisor"
)

// The customer window on Windows is a plain Win32 window. Fyne needs OpenGL,
// which most servers and VMs do not have (only the Basic/Remote Display
// Adapter), and there the Fyne window never appeared. Native controls draw
// everywhere. Everything that touches the window runs on the UI thread; work
// done in goroutines reports back through channels drained by a timer.

// Control IDs. Enter in a field arrives as IDOK through IsDialogMessage.
const (
	idOK = 1

	idPass1   = 101
	idPass2   = 102
	idSetNew  = 103
	idStart   = 104
	idStop    = 105
	idCopyCmd = 106
	idCopyCfg = 107

	timerRefresh = 1
)

var (
	colIdle     = rgb(140, 70, 40)
	colBusy     = rgb(40, 90, 160)
	colRunning  = rgb(20, 120, 70)
	colError    = rgb(170, 40, 40)
	theCustomer *nativeCustomer
)

type launchResult struct {
	session     orchestrator.Session
	err         error
	newPassword bool
}

type nativeCustomer struct {
	orch *orchestrator.Orchestrator
	b    provision.File
	user string

	hwnd, font, bold uintptr
	dpi              uint32
	statusColor      uint32

	host, status, intro, passLbl, pass1, pass2, setNew, hint uintptr
	start, stop, copyCmd, copyCfg, cmdLbl, cmd, logLbl, log  uintptr

	busy, wasRunning, closing bool
	runCancel                 context.CancelFunc
	// Incremental log: what has already been put into the log box, so new
	// lines are appended instead of replacing (and repainting) the whole box.
	logOrch       string // orchestrator text already shown
	helperPath    string
	helperOff     int64  // bytes of helper.log already shown
	helperPartial string // trailing helper bytes without a newline yet

	launchDone chan launchResult
	stopDone   chan struct{}
	closeDone  chan struct{}
	support    chan bool
}

func runCustomer(orch *orchestrator.Orchestrator, b provision.File) {
	runtime.LockOSThread()
	w := &nativeCustomer{
		orch:        orch,
		b:           b,
		user:        clientcfg.CurrentUser(),
		statusColor: colIdle,
		launchDone:  make(chan launchResult, 1),
		stopDone:    make(chan struct{}, 1),
		closeDone:   make(chan struct{}, 1),
		support:     make(chan bool, 1),
	}
	theCustomer = w
	if err := w.create(); err != nil {
		messageBox(0, "无法创建窗口: "+err.Error(), "CF SSH", mbOK|mbIconError)
		return
	}
	go func() {
		ok, _ := auth.SetPasswordSupport()
		w.support <- ok
	}()
	var m winMsg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		if d, _, _ := pIsDialogMessageW.Call(w.hwnd, uintptr(unsafe.Pointer(&m))); d != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (w *nativeCustomer) create() error {
	inst := moduleHandle()
	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)
	icon, _, _ := pLoadIconW.Call(0, idiApplication)
	brush, _, _ := pGetSysColorBrush.Call(colorWindow)
	class := u16("CFSSHCustomerWindow")
	wc := wndClassEx{
		WndProc:    windows.NewCallback(customerWndProc),
		Instance:   inst,
		Icon:       icon,
		Cursor:     cursor,
		Background: brush,
		ClassName:  class,
		IconSm:     icon,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return err
	}
	icc := struct{ Size, ICC uint32 }{8, iccStandardClasses}
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))

	w.dpi = systemDPI()
	hwnd, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(u16("CF SSH"))), wsOverlappedWindow|wsClipChildren,
		cwUseDefault, cwUseDefault, uintptr(w.px(760)), uintptr(w.px(780)), 0, 0, inst, 0)
	if hwnd == 0 {
		return err
	}
	w.hwnd = hwnd
	w.dpi = windowDPI(hwnd)
	w.makeFonts()

	label := func(text string, style uintptr) uintptr { return w.child("STATIC", text, ssNoPrefix|style, 0, 0) }
	w.host = label("公网地址："+w.b.Hostname, ssLeft)
	w.status = label("未运行", ssRight)
	w.intro = label(fmt.Sprintf("点启动后，这台电脑的 SSH 会通过 Cloudflare 安全地暴露到公网，关掉窗口就会停掉。\n当前用户：%s（%s/%s）", w.user, runtime.GOOS, runtime.GOARCH), ssLeft)
	w.passLbl = label("当前用户的登录密码", ssLeft)
	w.pass1 = w.child("EDIT", "", wsTabStop|esPassword|esAutoHScroll, wsExClientEdge, idPass1)
	w.pass2 = w.child("EDIT", "", wsTabStop|esPassword|esAutoHScroll, wsExClientEdge, idPass2)
	w.setNew = w.child("BUTTON", "不知道当前密码？给 "+w.user+" 设置一个新密码", wsTabStop|bsAutoCheckBox, 0, idSetNew)
	show(w.setNew, false)
	w.hint = label(passwordHint(), ssLeft)
	w.start = w.child("BUTTON", "启动", wsTabStop|bsDefPushButton, 0, idStart)
	w.stop = w.child("BUTTON", "停止", wsTabStop|bsPushButton, 0, idStop)
	w.copyCmd = w.child("BUTTON", "复制连接命令", wsTabStop|bsPushButton, 0, idCopyCmd)
	w.copyCfg = w.child("BUTTON", "复制 SSH 配置", wsTabStop|bsPushButton, 0, idCopyCfg)
	w.cmdLbl = label("连接命令", ssLeft)
	w.cmd = w.child("EDIT", "启动后这里会显示连接命令，发给要登录的人。对方电脑也要安装 cloudflared。",
		wsTabStop|wsVScroll|esMultiline|esReadOnly|esAutoVScroll, wsExClientEdge, 0)
	w.logLbl = label("日志", ssLeft)
	w.log = w.child("EDIT", "", wsTabStop|wsVScroll|esMultiline|esReadOnly|esAutoVScroll, wsExClientEdge, 0)
	enable(w.stop, false)
	w.applyMode()
	w.applyFonts()
	w.layout()

	pSetTimer.Call(hwnd, timerRefresh, 300, 0)
	pShowWindow.Call(hwnd, swShow)
	pUpdateWindow.Call(hwnd)
	pSetFocus.Call(w.pass1)
	return nil
}

func (w *nativeCustomer) child(class, text string, style, exStyle uintptr, id int) uintptr {
	h, _, _ := pCreateWindowExW.Call(exStyle, uintptr(unsafe.Pointer(u16(class))), uintptr(unsafe.Pointer(u16(text))),
		wsChild|wsVisible|style, 0, 0, 0, 0, w.hwnd, uintptr(id), moduleHandle(), 0)
	if class == "EDIT" || strings.Contains(text, "\n") {
		setText(h, text)
	}
	return h
}

// px scales a 96-DPI layout value to the window's DPI.
func (w *nativeCustomer) px(v int) int32 { return int32(v * int(w.dpi) / 96) }

func (w *nativeCustomer) makeFonts() {
	if w.font != 0 {
		pDeleteObject.Call(w.font)
		pDeleteObject.Call(w.bold)
	}
	w.font = newFont(10, fwNormal, w.dpi)
	w.bold = newFont(12, fwBold, w.dpi)
}

func (w *nativeCustomer) applyFonts() {
	for _, h := range []uintptr{w.host, w.intro, w.passLbl, w.pass1, w.pass2, w.setNew, w.hint,
		w.start, w.stop, w.copyCmd, w.copyCfg, w.cmdLbl, w.cmd, w.logLbl, w.log} {
		sendMsg(h, wmSetFont, w.font, 1)
	}
	sendMsg(w.status, wmSetFont, w.bold, 1)
}

func (w *nativeCustomer) layout() {
	var rc winRect
	pGetClientRect.Call(w.hwnd, uintptr(unsafe.Pointer(&rc)))
	cw, ch := rc.Right, rc.Bottom
	m := w.px(16)
	full := cw - 2*m
	move := func(h uintptr, x, y, width, height int32) {
		pMoveWindow.Call(h, uintptr(x), uintptr(y), uintptr(width), uintptr(height), 1)
	}
	y := w.px(12)
	statusW := w.px(190)
	move(w.host, m, y+w.px(3), full-statusW, w.px(24))
	move(w.status, cw-m-statusW, y, statusW, w.px(28))
	y += w.px(36)
	move(w.intro, m, y, full, w.px(44))
	y += w.px(52)
	move(w.passLbl, m, y, full, w.px(22))
	y += w.px(24)
	half := (full - w.px(12)) / 2
	move(w.pass1, m, y, half, w.px(28))
	move(w.pass2, m+half+w.px(12), y, half, w.px(28))
	y += w.px(36)
	move(w.setNew, m, y, full, w.px(24))
	y += w.px(28)
	move(w.hint, m, y, full, w.px(46))
	y += w.px(54)
	bw, bh, gap := w.px(96), w.px(32), w.px(8)
	cbw := w.px(132)
	move(w.start, m, y, bw, bh)
	move(w.stop, m+bw+gap, y, bw, bh)
	move(w.copyCfg, cw-m-cbw, y, cbw, bh)
	move(w.copyCmd, cw-m-2*cbw-gap, y, cbw, bh)
	y += bh + w.px(12)
	move(w.cmdLbl, m, y, full, w.px(22))
	y += w.px(24)
	move(w.cmd, m, y, full, w.px(92))
	y += w.px(100)
	move(w.logLbl, m, y, full, w.px(22))
	y += w.px(24)
	logH := ch - y - m
	if logH < w.px(60) {
		logH = w.px(60)
	}
	move(w.log, m, y, full, logH)
}

func (w *nativeCustomer) setStatus(text string, color uint32) {
	w.statusColor = color
	setText(w.status, text)
	pInvalidateRect.Call(w.status, 0, 0) // the static paints its own background
}

func (w *nativeCustomer) applyMode() {
	if checked(w.setNew) {
		setText(w.passLbl, "给 "+w.user+" 设置的新密码")
		setCue(w.pass1, "新密码")
		setText(w.hint, newPasswordHint(w.user))
	} else {
		setText(w.passLbl, "当前用户的登录密码")
		setCue(w.pass1, "当前用户的登录密码")
		setText(w.hint, passwordHint())
	}
	setCue(w.pass2, "再输入一次")
}

func (w *nativeCustomer) onCommand(id int) {
	switch id {
	case idStart, idOK:
		w.onStart()
	case idStop:
		w.onStop()
	case idSetNew:
		w.applyMode()
	case idCopyCmd:
		if setClipboard(w.hwnd, firstLine(getText(w.cmd))) {
			w.orch.Log("已复制连接命令")
		}
	case idCopyCfg:
		if setClipboard(w.hwnd, sshConfigBlock(getText(w.cmd))) {
			w.orch.Log("已复制 SSH 配置")
		}
	}
}

func (w *nativeCustomer) onStart() {
	if w.busy || w.closing || w.orch.Running() {
		return
	}
	pw := getText(w.pass1)
	newPassword := checked(w.setNew)
	if strings.TrimSpace(pw) == "" {
		if newPassword {
			w.orch.Log("请填写新密码")
		} else {
			w.orch.Log("请填写当前用户的登录密码")
		}
		return
	}
	if pw != getText(w.pass2) {
		w.orch.Log("两次输入的密码不一致")
		return
	}
	if newPassword {
		msg := "启动时会把 Windows 用户 " + w.user + " 的登录密码改成刚才输入的新密码。\n以后登录这台电脑（包括远程桌面）都要用新密码。\n\n确定继续吗？"
		if messageBox(w.hwnd, msg, "修改 Windows 登录密码", mbYesNo|mbIconWarning|mbDefButton2) != idYes {
			return
		}
	}
	w.busy = true
	enable(w.start, false)
	enable(w.stop, true)
	w.setStatus("正在启动…", colBusy)
	ctx, cancel := context.WithCancel(context.Background())
	w.runCancel = cancel
	go func() {
		s, err := w.orch.Launch(ctx, w.b, pw, newPassword)
		w.launchDone <- launchResult{session: s, err: err, newPassword: newPassword}
	}()
}

func (w *nativeCustomer) onLaunched(r launchResult) {
	w.busy = false
	w.runCancel = nil
	if r.err != nil {
		if errors.Is(r.err, orchestrator.ErrStopped) {
			w.setStatus("已停止", colIdle)
		} else {
			w.setStatus("启动失败", colError)
			w.orch.Log(r.err.Error())
		}
		enable(w.start, true)
		enable(w.stop, false)
		return
	}
	passNote := "刚才填写的当前用户密码"
	if r.newPassword {
		passNote = "刚才设置的新密码（也是这台电脑新的登录密码）"
		// The account now has this password; later starts just check it.
		setChecked(w.setNew, false)
		w.applyMode()
	}
	var text strings.Builder
	text.WriteString(r.session.Command)
	text.WriteString("\n\n")
	text.WriteString(r.session.SSHFile)
	fmt.Fprintf(&text, "\n登录用户: %s\nSSH 密码: %s\n", w.user, passNote)
	setText(w.cmd, text.String())
	w.wasRunning = true
	w.setStatus("运行中", colRunning)
	enable(w.stop, true)
}

func (w *nativeCustomer) onStop() {
	if w.runCancel != nil {
		w.runCancel()
	}
	enable(w.stop, false)
	go func() {
		w.orch.Stop()
		w.stopDone <- struct{}{}
	}()
}

func (w *nativeCustomer) requestClose() {
	if w.closing {
		return
	}
	w.closing = true
	w.setStatus("正在停止…", colIdle)
	enable(w.start, false)
	enable(w.stop, false)
	if w.runCancel != nil {
		w.runCancel()
	}
	go func() {
		done := make(chan struct{})
		go func() {
			w.orch.Stop()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(25 * time.Second):
		}
		w.closeDone <- struct{}{}
	}()
}

func (w *nativeCustomer) tick() {
	select {
	case ok := <-w.support:
		show(w.setNew, ok)
	default:
	}
	select {
	case r := <-w.launchDone:
		w.onLaunched(r)
	default:
	}
	select {
	case <-w.stopDone:
		w.busy = false
		w.wasRunning = false
		enable(w.start, true)
		enable(w.stop, false)
		w.setStatus("已停止", colIdle)
	default:
	}
	select {
	case <-w.closeDone:
		pDestroyWindow.Call(w.hwnd)
		return
	default:
	}

	w.appendLog()
	if w.closing || w.busy {
		return
	}
	if w.orch.Running() {
		w.wasRunning = true
		return
	}
	// The tunnel or sshd died on its own: say so and allow a new start.
	if w.wasRunning {
		w.wasRunning = false
		if st, ok := w.orch.LastStatus(); ok && st.State == "error" {
			w.setStatus("已断开", colError)
			w.orch.Log(st.Detail)
		} else {
			w.setStatus("已停止", colIdle)
		}
		enable(w.start, true)
		enable(w.stop, false)
	}
}

func (w *nativeCustomer) onDPIChanged(dpi uint32, suggested winRect) {
	w.dpi = dpi
	w.makeFonts()
	w.applyFonts()
	pSetWindowPos.Call(w.hwnd, 0, uintptr(suggested.Left), uintptr(suggested.Top),
		uintptr(suggested.Right-suggested.Left), uintptr(suggested.Bottom-suggested.Top), swpNoZOrder|swpNoActivate)
	w.layout()
}

func customerWndProc(hwnd, msg, wp, lp uintptr) uintptr {
	w := theCustomer
	if w == nil || w.hwnd == 0 {
		r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
		return r
	}
	switch uint32(msg) {
	case wmCommand:
		if (wp>>16)&0xffff == bnClicked {
			w.onCommand(int(wp & 0xffff))
		}
		return 0
	case wmTimer:
		w.tick()
		return 0
	case wmSize:
		w.layout()
		return 0
	case wmGetMinMaxInfo:
		var mmi minMaxInfo
		pRtlMoveMemory.Call(uintptr(unsafe.Pointer(&mmi)), lp, unsafe.Sizeof(mmi))
		mmi.MinTrackSize.X, mmi.MinTrackSize.Y = w.px(640), w.px(680)
		pRtlMoveMemory.Call(lp, uintptr(unsafe.Pointer(&mmi)), unsafe.Sizeof(mmi))
		return 0
	case wmCtlColorStatic:
		// Labels and read-only edits: window background, readable dark text,
		// and a state colour for the status line.
		bg, _, _ := pGetSysColor.Call(colorWindow)
		pSetBkColor.Call(wp, bg)
		if lp == w.status {
			pSetTextColor.Call(wp, uintptr(w.statusColor))
		} else {
			fg, _, _ := pGetSysColor.Call(colorWindowText)
			pSetTextColor.Call(wp, fg)
		}
		brush, _, _ := pGetSysColorBrush.Call(colorWindow)
		return brush
	case wmDpiChanged:
		var rc winRect
		pRtlMoveMemory.Call(uintptr(unsafe.Pointer(&rc)), lp, unsafe.Sizeof(rc))
		w.onDPIChanged(uint32(wp&0xffff), rc)
		return 0
	case wmClose:
		w.requestClose()
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

const maxLogChars = 400_000

// appendLog adds only what is new since the last tick. Replacing the whole
// edit text every few hundred milliseconds made the box erase, redraw and jump
// back to the top each time, which flickered badly while starting.
func (w *nativeCustomer) appendLog() {
	var lines []string
	if t := w.orch.Text(); t != w.logOrch {
		added := t
		if strings.HasPrefix(t, w.logOrch) {
			added = strings.TrimPrefix(t[len(w.logOrch):], "\n")
		}
		w.logOrch = t
		if added != "" {
			lines = append(lines, strings.Split(added, "\n")...)
		}
	}
	lines = append(lines, w.readHelperLines()...)
	if len(lines) == 0 {
		return
	}
	n, _, _ := pGetWindowTextLengthW.Call(w.log)
	chunk := strings.Join(lines, "\r\n")
	if n > 0 {
		chunk = "\r\n" + chunk
	}
	if int(n)+len(chunk) > maxLogChars {
		// Rare: trim to the newer half in one go, with painting suspended.
		old := getText(w.log)
		keep := old[len(old)/2:]
		if i := strings.Index(keep, "\r\n"); i >= 0 {
			keep = keep[i+2:]
		}
		sendMsg(w.log, wmSetRedraw, 0, 0)
		setText(w.log, keep+chunk)
		sendMsg(w.log, wmSetRedraw, 1, 0)
		pInvalidateRect.Call(w.log, 0, 1)
		n, _, _ = pGetWindowTextLengthW.Call(w.log)
		sendMsg(w.log, emSetSel, n, n)
		sendMsg(w.log, emScrollCaret, 0, 0)
		return
	}
	sendMsg(w.log, emSetSel, n, n)
	sendMsg(w.log, emReplaceSel, 0, uintptr(unsafe.Pointer(u16(chunk))))
	sendMsg(w.log, emScrollCaret, 0, 0)
}

// readHelperLines returns complete new lines from the elevated helper's log.
func (w *nativeCustomer) readHelperLines() []string {
	dir := w.orch.RunDir()
	if dir == "" {
		return nil
	}
	path := supervisor.LogPath(dir)
	if path != w.helperPath {
		w.helperPath, w.helperOff, w.helperPartial = path, 0, ""
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	if st.Size() < w.helperOff { // a new run truncated the log
		w.helperOff, w.helperPartial = 0, ""
	}
	if st.Size() == w.helperOff {
		return nil
	}
	buf := make([]byte, st.Size()-w.helperOff)
	n, _ := f.ReadAt(buf, w.helperOff)
	w.helperOff += int64(n)
	text := w.helperPartial + strings.ReplaceAll(string(buf[:n]), "\r\n", "\n")
	i := strings.LastIndex(text, "\n")
	if i < 0 {
		w.helperPartial = text
		return nil
	}
	w.helperPartial = text[i+1:]
	return strings.Split(text[:i], "\n")
}
