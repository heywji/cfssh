//go:build windows

package auth

import (
	"fmt"
	"os"
	"os/user"
	"unsafe"

	"golang.org/x/sys/windows"

	"cfssh/internal/clientcfg"
)

var logonUserW = windows.NewLazySystemDLL("advapi32.dll").NewProc("LogonUserW")

const (
	logonNetwork     = 3
	logonInteractive = 2
	providerDefault  = 0
)

func verify(password string) error {
	name := clientcfg.CurrentUser()
	var users []string
	if name != "" {
		users = append(users, name)
	}
	if current, err := user.Current(); err == nil && current.Username != "" && current.Username != name {
		users = append(users, current.Username)
	}
	domains := unique([]string{
		".",
		os.Getenv("COMPUTERNAME"),
		os.Getenv("USERDOMAIN"),
		"MicrosoftAccount",
	})
	for _, account := range users {
		for _, domain := range domains {
			if tryLogon(account, domain, password) {
				return nil
			}
		}
	}
	shown := name
	if shown == "" {
		shown = "当前用户"
	}
	return fmt.Errorf("这个密码不能登录 Windows 用户 %s。客户端 SSH 时要填同一个密码", shown)
}

func tryLogon(account, domain, password string) bool {
	userPtr, err := windows.UTF16PtrFromString(account)
	if err != nil {
		return false
	}
	domainPtr, err := windows.UTF16PtrFromString(domain)
	if err != nil {
		return false
	}
	passPtr, err := windows.UTF16PtrFromString(password)
	if err != nil {
		return false
	}
	for _, kind := range []uint32{logonNetwork, logonInteractive} {
		token, err := callLogonUser(userPtr, domainPtr, passPtr, kind)
		if err == nil {
			_ = token.Close()
			return true
		}
	}
	return false
}

func callLogonUser(account, domain, password *uint16, kind uint32) (windows.Token, error) {
	var token windows.Handle
	r, _, err := logonUserW.Call(
		uintptr(unsafe.Pointer(account)),
		uintptr(unsafe.Pointer(domain)),
		uintptr(unsafe.Pointer(password)),
		uintptr(kind),
		uintptr(providerDefault),
		uintptr(unsafe.Pointer(&token)),
	)
	if r == 0 {
		if err == nil {
			err = windows.ERROR_LOGON_FAILURE
		}
		return 0, err
	}
	return windows.Token(token), nil
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range in {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}
