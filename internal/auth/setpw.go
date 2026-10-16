package auth

import (
	"errors"
	"strings"
)

// Some users do not know their own login password. When they are an
// administrator of this machine, cfssh can give the account a new password
// instead of checking the old one. Only Windows local accounts are supported:
// a domain or Microsoft account password cannot be set from this machine.

// SetPasswordSupport reports whether the current account can be given a new
// password, and if not, why. It may take a moment (it asks Windows).
func SetPasswordSupport() (bool, string) {
	return setPasswordSupport()
}

// SealPassword encrypts a new password so only the current user can read it
// back (Windows DPAPI). The sealed text is what travels to the elevated helper,
// so the plain password never touches the disk.
func SealPassword(password string) (string, error) {
	if strings.TrimSpace(password) == "" {
		return "", errors.New("请填写新密码")
	}
	return sealPassword(password)
}

// ApplySealedPassword decrypts a password produced by SealPassword and sets it
// as the login password of user. It must run elevated, as the same user that
// sealed it.
func ApplySealedPassword(user, sealed string) error {
	if user == "" || sealed == "" {
		return errors.New("缺少要设置密码的用户名或密码")
	}
	return applySealedPassword(user, sealed)
}
