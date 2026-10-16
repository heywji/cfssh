//go:build windows

package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"cfssh/internal/clientcfg"

	"cfssh/internal/hidewin"
)

var netUserSetInfo = windows.NewLazySystemDLL("netapi32.dll").NewProc("NetUserSetInfo")

const (
	nerrUserNotFound      = 2221
	nerrPasswordTooShort  = 2245 // also returned for complexity and history failures
	errorAccessDenied     = 5
	userInfo1003PasswdLvl = 1003
)

func setPasswordSupport() (bool, string) {
	if !isAdministrator() {
		return false, "当前用户不是这台电脑的管理员，不能在这里设置新密码"
	}
	domain, computer := os.Getenv("USERDOMAIN"), os.Getenv("COMPUTERNAME")
	if domain != "" && computer != "" && !strings.EqualFold(domain, computer) {
		return false, "当前用户是域账户（" + domain + "），密码要在域里修改"
	}
	if isMicrosoftAccount(clientcfg.CurrentUser()) {
		return false, "当前用户登录的是微软账户，密码要在微软账户里修改"
	}
	return true, ""
}

// isAdministrator is true when BUILTIN\Administrators is in the token at all.
// Under UAC the group is present but deny-only until elevation; the helper
// elevates later, so membership is what matters here.
func isAdministrator() bool {
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return false
	}
	groups, err := windows.GetCurrentProcessToken().GetTokenGroups()
	if err != nil {
		return false
	}
	for _, g := range groups.AllGroups() {
		if g.Sid.Equals(admins) {
			return true
		}
	}
	return false
}

func isMicrosoftAccount(name string) bool {
	if name == "" {
		return false
	}
	quoted := "'" + strings.ReplaceAll(name, "'", "''") + "'"
	cmd := hidewin.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"(Get-LocalUser -Name "+quoted+" -ErrorAction SilentlyContinue).PrincipalSource")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "MicrosoftAccount")
}

func sealPassword(password string) (string, error) {
	in := []byte(password)
	defer zero(in)
	var out windows.DataBlob
	err := windows.CryptProtectData(&windows.DataBlob{Size: uint32(len(in)), Data: &in[0]},
		nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return "", fmt.Errorf("加密新密码失败: %w", err)
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size)), nil
}

func applySealedPassword(user, sealed string) error {
	blob, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(blob) == 0 {
		return errors.New("新密码数据损坏")
	}
	var out windows.DataBlob
	err = windows.CryptUnprotectData(&windows.DataBlob{Size: uint32(len(blob)), Data: &blob[0]},
		nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	if err != nil {
		return fmt.Errorf("解不开新密码，需要由同一个 Windows 用户来设置: %w", err)
	}
	plain := unsafe.Slice(out.Data, out.Size)
	pw16, convErr := windows.UTF16FromString(string(plain))
	zero(plain)
	windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	if convErr != nil {
		return errors.New("新密码里有不支持的字符")
	}
	defer func() {
		for i := range pw16 {
			pw16[i] = 0
		}
	}()
	userPtr, err := windows.UTF16PtrFromString(user)
	if err != nil {
		return err
	}
	info := struct{ password *uint16 }{&pw16[0]} // USER_INFO_1003
	var parmErr uint32
	r, _, _ := netUserSetInfo.Call(0, uintptr(unsafe.Pointer(userPtr)), userInfo1003PasswdLvl,
		uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Pointer(&parmErr)))
	runtime.KeepAlive(pw16)
	switch r {
	case 0:
		return nil
	case nerrPasswordTooShort:
		return errors.New("新密码不符合这台电脑的密码要求（长度、复杂度，或不能和最近用过的密码相同）")
	case nerrUserNotFound:
		return fmt.Errorf("找不到 Windows 用户 %s", user)
	case errorAccessDenied:
		return errors.New("没有权限修改密码，需要管理员权限")
	default:
		return fmt.Errorf("设置新密码失败，错误码 %d", r)
	}
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
