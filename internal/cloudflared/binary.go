package cloudflared

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"cfssh/internal/hidewin"
)

const releaseBase = "https://github.com/cloudflare/cloudflared/releases/latest/download/"

// Ensure returns a usable cloudflared path, downloading the official release
// into dir when the cache is empty and the system PATH does not have one.
func Ensure(ctx context.Context, dir string, logf func(string)) (string, error) {
	cached := cachedPath(dir)
	if fileExists(cached) {
		return cached, nil
	}
	if path, err := exec.LookPath("cloudflared"); err == nil {
		logf("使用系统里的 cloudflared: " + path)
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := assetName()
	url := releaseBase + name
	logf("正在下载 cloudflared: " + url)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 cloudflared 失败: HTTP %d", resp.StatusCode)
	}
	tmp := cached + ".download"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if strings.HasSuffix(name, ".tgz") {
		if err := extractSingle(tmp, cached); err != nil {
			return "", err
		}
		_ = os.Remove(tmp)
	} else if err := os.Rename(tmp, cached); err != nil {
		return "", err
	}
	if err := os.Chmod(cached, 0o755); err != nil {
		return "", err
	}
	out, err := hidewin.Command(cached, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("cloudflared 无法运行: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	logf(strings.TrimSpace(string(out)))
	return cached, nil
}

func cachedPath(dir string) string {
	name := "cloudflared"
	if runtime.GOOS == "windows" {
		name = "cloudflared.exe"
	}
	return filepath.Join(dir, name)
}

func assetName() string {
	arch := runtime.GOARCH
	switch runtime.GOOS {
	case "windows":
		return "cloudflared-windows-" + arch + ".exe"
	case "darwin":
		return "cloudflared-darwin-" + arch + ".tgz"
	default:
		return "cloudflared-linux-" + arch
	}
}

func extractSingle(tgzPath, dest string) error {
	f, err := os.Open(tgzPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(hdr.Name)
		if base != "cloudflared" && base != "cloudflared.exe" {
			continue
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
	return errors.New("压缩包里没有 cloudflared")
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
