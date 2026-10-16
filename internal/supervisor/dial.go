package supervisor

import (
	"net"
	"time"
)

func dial(addr string) (net.Conn, error) {
	return net.DialTimeout("tcp", addr, 400*time.Millisecond)
}
