package auth

import "strings"

// VerifyCurrentUser checks that password is the login password of the account
// that is running the app. SSH password authentication uses that same password.
// The password is not stored.
func VerifyCurrentUser(password string) error {
	if password == "" || strings.TrimSpace(password) == "" {
		return errEmptyPassword
	}
	return verify(password)
}
