package supervisor

import (
	"fmt"
	"net"
)

// PickPort returns preferred when it is free on the loopback address, otherwise
// the next free port in 2222-2299.
func PickPort(preferred int) (int, error) {
	candidates := make([]int, 0, 80)
	if preferred > 0 && preferred < 65536 {
		candidates = append(candidates, preferred)
	}
	for p := 2222; p <= 2299; p++ {
		if p != preferred {
			candidates = append(candidates, p)
		}
	}
	for _, p := range candidates {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			continue
		}
		_ = ln.Close()
		return p, nil
	}
	return 0, fmt.Errorf("127.0.0.1 上 2222-2299 都已被占用")
}
