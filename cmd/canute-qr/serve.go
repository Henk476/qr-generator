package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/Henk476/qr-generator/internal/web"
)

func runServe(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	addr := fs.String("addr", "127.0.0.1:8080", "address to listen on; keep it on 127.0.0.1 unless you know you need otherwise")
	dataDir := fs.String("data-dir", "", dataDirUsage)
	open := fs.Bool("open", false, "open the UI in your default browser")
	allowedHosts := fs.String("allowed-hosts", "", `comma-separated Host values to accept, e.g. "localhost:8080,qr.home.lan:8080" (default: the loopback names when --addr is loopback, otherwise any)`)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "canute-qr: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "canute-qr:", err)
		return 1
	}

	st, dir, err := openStore(*dataDir)
	if err != nil {
		return fail(err)
	}
	defer st.Close()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return fail(err)
	}
	host, _, _ := net.SplitHostPort(*addr)
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	var allowed []string
	urlHost := host
	switch {
	case *allowedHosts != "":
		for _, h := range strings.Split(*allowedHosts, ",") {
			if h = strings.TrimSpace(h); h != "" {
				allowed = append(allowed, h)
			}
		}
	case isLoopback(host):
		// Only accept our own host names: blocks DNS rebinding from web pages.
		allowed = []string{"127.0.0.1:" + port, "localhost:" + port, "[::1]:" + port}
	}
	if !isLoopback(host) {
		fmt.Fprintf(stderr, "warning: listening on %s, so other devices on your network can reach this UI. It has no password.\n", ln.Addr())
		if len(allowed) == 0 {
			fmt.Fprintln(stderr, "warning: any Host header is accepted; set --allowed-hosts to block DNS-rebinding attacks.")
		}
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "localhost" {
		urlHost = "127.0.0.1"
	}
	url := "http://" + net.JoinHostPort(urlHost, port)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ui, err := web.New(ctx, st, web.Options{AllowedHosts: allowed, Version: version})
	if err != nil {
		return fail(err)
	}
	srv := &http.Server{
		Handler:           ui,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()

	fmt.Fprintf(stdout, "Canute's QR Generator is running at %s (Ctrl+C to stop)\nData directory: %s\n", url, dir)
	if *open {
		if err := openBrowser(url); err != nil {
			fmt.Fprintln(stderr, "warning: could not open a browser:", err)
		}
	}
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return fail(err)
	}
	return 0
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
