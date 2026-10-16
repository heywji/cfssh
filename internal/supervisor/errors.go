package supervisor

import "errors"

var (
	errProcExited = errors.New("进程已退出")
	errTimeout    = errors.New("等待端口超时")
)
