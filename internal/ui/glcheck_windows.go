//go:build windows

package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// checkGraphics runs before the Fyne (advanced) window. Fyne needs a real
// OpenGL driver; on servers and VMs Windows only offers "GDI Generic" OpenGL
// 1.1, Fyne logs "WGL: The driver does not appear to support OpenGL" and the
// process stays alive with no window at all. Probe first and explain instead.
func checkGraphics() error {
	if exe, err := os.Executable(); err == nil {
		if _, err := os.Stat(filepath.Join(filepath.Dir(exe), "opengl32.dll")); err == nil {
			return nil // a bundled software OpenGL (e.g. Mesa) is the user's explicit choice
		}
	}
	renderer, err := probeOpenGL()
	if err == nil && !strings.Contains(renderer, "GDI Generic") {
		return nil
	}
	return errors.New("这台电脑没有可用的 OpenGL 显卡驱动（常见于云服务器和虚拟机），高级界面无法显示。\n\n" +
		"• 客户使用：把运营方给的 cfssh-provision.json 放在 cfssh.exe 旁边，客户窗口不需要 OpenGL。\n" +
		"• 运营方：在有显卡驱动的电脑上打开高级界面，或使用命令行 cfssh provision。")
}

func showFatal(msg string) { messageBox(0, msg, "CF SSH", mbOK|mbIconError) }

type pixelFormatDescriptor struct {
	Size, Version                                  uint16
	Flags                                          uint32
	PixelType, ColorBits, RedBits, RedShift        byte
	GreenBits, GreenShift, BlueBits, BlueShift     byte
	AlphaBits, AlphaShift, AccumBits, AccumRedBits byte
	AccumGreenBits, AccumBlueBits, AccumAlphaBits  byte
	DepthBits, StencilBits, AuxBuffers, LayerType  byte
	Reserved                                       byte
	LayerMask, VisibleMask, DamageMask             uint32
}

func probeOpenGL() (string, error) {
	opengl32 := windows.NewLazySystemDLL("opengl32.dll")
	wglCreateContext := opengl32.NewProc("wglCreateContext")
	wglMakeCurrent := opengl32.NewProc("wglMakeCurrent")
	wglDeleteContext := opengl32.NewProc("wglDeleteContext")
	glGetString := opengl32.NewProc("glGetString")
	choosePixelFormat := gdi32.NewProc("ChoosePixelFormat")
	setPixelFormat := gdi32.NewProc("SetPixelFormat")
	if err := opengl32.Load(); err != nil {
		return "", err
	}

	inst := moduleHandle()
	class := u16("CFSSHGLProbe")
	wc := wndClassEx{WndProc: pDefWindowProcW.Addr(), Instance: inst, ClassName: class}
	wc.Size = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	defer pUnregisterClassW.Call(uintptr(unsafe.Pointer(class)), inst)
	hwnd, _, err := pCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), 0, wsOverlappedWindow, 0, 0, 1, 1, 0, 0, inst, 0)
	if hwnd == 0 {
		return "", err
	}
	defer pDestroyWindow.Call(hwnd)
	hdc, _, _ := pGetDC.Call(hwnd)
	if hdc == 0 {
		return "", errors.New("GetDC failed")
	}
	defer pReleaseDC.Call(hwnd, hdc)

	pfd := pixelFormatDescriptor{Version: 1, Flags: 0x4 | 0x20 | 0x1, ColorBits: 32, DepthBits: 24}
	pfd.Size = uint16(unsafe.Sizeof(pfd))
	pf, _, _ := choosePixelFormat.Call(hdc, uintptr(unsafe.Pointer(&pfd)))
	if pf == 0 {
		return "", errors.New("no OpenGL pixel format")
	}
	if r, _, _ := setPixelFormat.Call(hdc, pf, uintptr(unsafe.Pointer(&pfd))); r == 0 {
		return "", errors.New("SetPixelFormat failed")
	}
	ctx, _, _ := wglCreateContext.Call(hdc)
	if ctx == 0 {
		return "", errors.New("wglCreateContext failed")
	}
	defer wglDeleteContext.Call(ctx)
	if r, _, _ := wglMakeCurrent.Call(hdc, ctx); r == 0 {
		return "", errors.New("wglMakeCurrent failed")
	}
	defer wglMakeCurrent.Call(0, 0)
	const glRenderer = 0x1F01
	p, _, _ := glGetString.Call(glRenderer)
	if p == 0 {
		return "", errors.New("glGetString failed")
	}
	return cString(p), nil
}

// cString copies a NUL-terminated C string without converting the address
// back into a Go pointer (which go vet rightly distrusts).
func cString(addr uintptr) string {
	var out []byte
	var b byte
	for i := uintptr(0); i < 256; i++ {
		pRtlMoveMemory.Call(uintptr(unsafe.Pointer(&b)), addr+i, 1)
		if b == 0 {
			break
		}
		out = append(out, b)
	}
	return string(out)
}
