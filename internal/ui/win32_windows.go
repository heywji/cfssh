//go:build windows

package ui

// Thin bindings for the few Win32 calls the native customer window needs.
// They go straight through user32/gdi32/kernel32 so the window works on any
// Windows, including servers and VMs without an OpenGL driver.

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	comctl32 = windows.NewLazySystemDLL("comctl32.dll")

	pRegisterClassExW     = user32.NewProc("RegisterClassExW")
	pUnregisterClassW     = user32.NewProc("UnregisterClassW")
	pCreateWindowExW      = user32.NewProc("CreateWindowExW")
	pDefWindowProcW       = user32.NewProc("DefWindowProcW")
	pDestroyWindow        = user32.NewProc("DestroyWindow")
	pGetMessageW          = user32.NewProc("GetMessageW")
	pTranslateMessage     = user32.NewProc("TranslateMessage")
	pDispatchMessageW     = user32.NewProc("DispatchMessageW")
	pIsDialogMessageW     = user32.NewProc("IsDialogMessageW")
	pPostQuitMessage      = user32.NewProc("PostQuitMessage")
	pSendMessageW         = user32.NewProc("SendMessageW")
	pSetWindowTextW       = user32.NewProc("SetWindowTextW")
	pGetWindowTextW       = user32.NewProc("GetWindowTextW")
	pGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	pEnableWindow         = user32.NewProc("EnableWindow")
	pShowWindow           = user32.NewProc("ShowWindow")
	pUpdateWindow         = user32.NewProc("UpdateWindow")
	pMoveWindow           = user32.NewProc("MoveWindow")
	pSetWindowPos         = user32.NewProc("SetWindowPos")
	pGetClientRect        = user32.NewProc("GetClientRect")
	pSetTimer             = user32.NewProc("SetTimer")
	pLoadCursorW          = user32.NewProc("LoadCursorW")
	pLoadIconW            = user32.NewProc("LoadIconW")
	pMessageBoxW          = user32.NewProc("MessageBoxW")
	pGetDpiForWindow      = user32.NewProc("GetDpiForWindow")
	pGetDpiForSystem      = user32.NewProc("GetDpiForSystem")
	pGetSysColor          = user32.NewProc("GetSysColor")
	pGetSysColorBrush     = user32.NewProc("GetSysColorBrush")
	pInvalidateRect       = user32.NewProc("InvalidateRect")
	pSetFocus             = user32.NewProc("SetFocus")
	pOpenClipboard        = user32.NewProc("OpenClipboard")
	pEmptyClipboard       = user32.NewProc("EmptyClipboard")
	pSetClipboardData     = user32.NewProc("SetClipboardData")
	pCloseClipboard       = user32.NewProc("CloseClipboard")
	pGetDC                = user32.NewProc("GetDC")
	pReleaseDC            = user32.NewProc("ReleaseDC")
	pCreateFontW          = gdi32.NewProc("CreateFontW")
	pDeleteObject         = gdi32.NewProc("DeleteObject")
	pSetTextColor         = gdi32.NewProc("SetTextColor")
	pSetBkColor           = gdi32.NewProc("SetBkColor")
	pGetModuleHandleW     = kernel32.NewProc("GetModuleHandleW")
	pGlobalAlloc          = kernel32.NewProc("GlobalAlloc")
	pGlobalLock           = kernel32.NewProc("GlobalLock")
	pGlobalUnlock         = kernel32.NewProc("GlobalUnlock")
	pGlobalFree           = kernel32.NewProc("GlobalFree")
	pRtlMoveMemory        = kernel32.NewProc("RtlMoveMemory")
	pInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
)

const (
	wsOverlappedWindow = 0x00CF0000
	wsVisible          = 0x10000000
	wsChild            = 0x40000000
	wsTabStop          = 0x00010000
	wsVScroll          = 0x00200000
	wsClipChildren     = 0x02000000
	wsExClientEdge     = 0x00000200

	esMultiline   = 0x0004
	esPassword    = 0x0020
	esAutoVScroll = 0x0040
	esAutoHScroll = 0x0080
	esReadOnly    = 0x0800

	bsPushButton    = 0x0
	bsDefPushButton = 0x1
	bsAutoCheckBox  = 0x3
	ssLeft          = 0x0
	ssRight         = 0x2
	ssNoPrefix      = 0x80

	wmDestroy        = 0x0002
	wmSize           = 0x0005
	wmClose          = 0x0010
	wmSetFont        = 0x0030
	wmGetMinMaxInfo  = 0x0024
	wmCommand        = 0x0111
	wmTimer          = 0x0113
	wmCtlColorEdit   = 0x0133
	wmCtlColorStatic = 0x0138
	wmDpiChanged     = 0x02E0

	bnClicked      = 0
	bmGetCheck     = 0x00F0
	bmSetCheck     = 0x00F1
	bstChecked     = 1
	emSetSel       = 0x00B1
	emScrollCaret  = 0x00B7
	emSetCueBanner = 0x1501

	swHide = 0
	swShow = 5

	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	cwUseDefault    = 0x80000000
	idcArrow        = 32512
	idiApplication  = 32512
	colorWindow     = 5
	colorWindowText = 8

	mbOK          = 0x0
	mbYesNo       = 0x4
	mbIconError   = 0x10
	mbIconWarning = 0x30
	mbDefButton2  = 0x100
	idYes         = 6

	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	iccStandardClasses = 0x4000
	fwNormal           = 400
	fwBold             = 700
	defaultCharset     = 1
	clearTypeQuality   = 5
)

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type winMsg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	PtX, PtY int32
	LPrivate uint32
}

type winRect struct{ Left, Top, Right, Bottom int32 }

type minMaxInfo struct {
	Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize struct{ X, Y int32 }
}

func u16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(strings.ReplaceAll(s, "\x00", ""))
	return p
}

func moduleHandle() uintptr {
	h, _, _ := pGetModuleHandleW.Call(0)
	return h
}

func sendMsg(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := pSendMessageW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func setText(hwnd uintptr, s string) {
	// Edit controls need CRLF line breaks.
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	pSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(u16(s))))
}

func getText(hwnd uintptr) string {
	n, _, _ := pGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return windows.UTF16ToString(buf)
}

func enable(hwnd uintptr, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	pEnableWindow.Call(hwnd, v)
}

func show(hwnd uintptr, on bool) {
	v := uintptr(swHide)
	if on {
		v = swShow
	}
	pShowWindow.Call(hwnd, v)
}

func checked(hwnd uintptr) bool { return sendMsg(hwnd, bmGetCheck, 0, 0) == bstChecked }

func setChecked(hwnd uintptr, on bool) {
	v := uintptr(0)
	if on {
		v = bstChecked
	}
	sendMsg(hwnd, bmSetCheck, v, 0)
}

func setCue(hwnd uintptr, s string) {
	sendMsg(hwnd, emSetCueBanner, 1, uintptr(unsafe.Pointer(u16(s))))
}

func messageBox(owner uintptr, text, title string, flags uint32) int {
	r, _, _ := pMessageBoxW.Call(owner, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), uintptr(flags))
	return int(r)
}

func systemDPI() uint32 {
	if pGetDpiForSystem.Find() == nil {
		if d, _, _ := pGetDpiForSystem.Call(); d != 0 {
			return uint32(d)
		}
	}
	return 96
}

func windowDPI(hwnd uintptr) uint32 {
	if pGetDpiForWindow.Find() == nil {
		if d, _, _ := pGetDpiForWindow.Call(hwnd); d != 0 {
			return uint32(d)
		}
	}
	return systemDPI()
}

func newFont(pt int, weight int, dpi uint32) uintptr {
	height := -int32(pt * int(dpi) / 72)
	h, _, _ := pCreateFontW.Call(uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0,
		defaultCharset, 0, 0, clearTypeQuality, 0, uintptr(unsafe.Pointer(u16("Microsoft YaHei UI"))))
	return h
}

func rgb(r, g, b uint8) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }

func setClipboard(owner uintptr, s string) bool {
	if r, _, _ := pOpenClipboard.Call(owner); r == 0 {
		return false
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	data := windows.StringToUTF16(s)
	size := uintptr(len(data) * 2)
	h, _, _ := pGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return false
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		pGlobalFree.Call(h)
		return false
	}
	pRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&data[0])), size)
	pGlobalUnlock.Call(h)
	if r, _, _ := pSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		pGlobalFree.Call(h)
		return false
	}
	return true
}
