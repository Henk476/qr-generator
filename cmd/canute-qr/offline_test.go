package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestNoOutboundNetworkCalls guards the "no network calls" promise: no
// program code may create an HTTP client, make HTTP requests or dial out.
// Listening (net.Listen, http.Server) is fine; it only accepts connections.
func TestNoOutboundNetworkCalls(t *testing.T) {
	forbidden := regexp.MustCompile(`http\.(Get|Post|PostForm|Head|NewRequest|NewRequestWithContext|DefaultClient|DefaultTransport)\b|http\.Client\{|&http\.Transport\{|net\.Dial|\.DialContext\(|tls\.Dial`)
	root := filepath.Join("..", "..")
	var checked int
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "dist" || d.Name() == ".claude") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checked++
		for i, line := range strings.Split(string(b), "\n") {
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "//") {
				continue
			}
			if m := forbidden.FindString(line); m != "" {
				t.Errorf("%s:%d makes an outbound network call (%s)", path, i+1, m)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 30 {
		t.Fatalf("only %d Go files checked; the walk is broken", checked)
	}
}
