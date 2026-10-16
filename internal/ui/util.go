package ui

import (
	"net/url"
	"os"
)

func parseURL(raw string) (*url.URL, error) {
	return url.Parse(raw)
}

func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
