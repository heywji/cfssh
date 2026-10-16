//go:build !windows

package auth

import "errors"

var errSetPasswordUnsupported = errors.New("只有 Windows 支持在这里设置新密码")

func setPasswordSupport() (bool, string) {
	return false, errSetPasswordUnsupported.Error()
}

func sealPassword(string) (string, error) { return "", errSetPasswordUnsupported }

func applySealedPassword(string, string) error { return errSetPasswordUnsupported }
