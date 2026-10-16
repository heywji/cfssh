package auth

import "errors"

var errEmptyPassword = errors.New("请填写当前用户的 SSH 登录密码")
