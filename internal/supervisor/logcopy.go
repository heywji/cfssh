package supervisor

import (
	"bufio"
	"io"
)

func copyLog(r io.Reader, logf func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		logf(sc.Text())
	}
}
