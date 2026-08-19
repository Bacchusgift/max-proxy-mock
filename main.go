package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"max-proxy-mock/internal/admin"
	"max-proxy-mock/internal/certificate"
	proxyServer "max-proxy-mock/internal/proxy"
	appRuntime "max-proxy-mock/internal/runtime"
	"max-proxy-mock/internal/storage"
)

//go:embed web/dist
var webFiles embed.FS

func main() {
	proxyPort := flag.Int("proxy-port", 8899, "proxy port")
	adminPort := flag.Int("admin-port", 8900, "admin port")
	dataDir := flag.String("data-dir", "data", "local data directory")
	flag.Parse()
	if err := os.MkdirAll(*dataDir, 0700); err != nil {
		fatal(err)
	}
	store, err := storage.Open(filepath.Join(*dataDir, "max-proxy-mock.db"))
	if err != nil {
		fatal(err)
	}
	defer store.Close()
	ca, caPath, err := certificate.LoadOrCreate(filepath.Join(*dataDir, "certificates"))
	if err != nil {
		fatal(err)
	}
	state := appRuntime.New()
	pacURL := fmt.Sprintf("http://127.0.0.1:%d/proxy.pac", *adminPort)
	proxyAddress := fmt.Sprintf("127.0.0.1:%d", *proxyPort)
	api := admin.New(store, state, caPath, pacURL, proxyAddress)
	if err := api.Bootstrap(); err != nil {
		fatal(err)
	}

	proxyHTTP := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", *proxyPort), Handler: proxyServer.New(store, state, &ca), ReadHeaderTimeout: 10 * time.Second}
	mux := http.NewServeMux()
	api.Register(mux)
	dist, _ := fs.Sub(webFiles, "web/dist")
	mux.Handle("/", spa(http.FS(dist)))
	adminHTTP := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", *adminPort), Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		slog.Info("代理已启动", "address", proxyHTTP.Addr)
		if err := proxyHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fatal(err)
		}
	}()
	go func() {
		slog.Info("管理页面已启动", "url", "http://"+adminHTTP.Addr)
		if err := adminHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fatal(err)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	_ = proxyHTTP.Close()
	_ = adminHTTP.Close()
}

func spa(files http.FileSystem) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, err := files.Open(strings.TrimPrefix(r.URL.Path, "/"))
		if err == nil {
			_ = f.Close()
			http.FileServer(files).ServeHTTP(w, r)
			return
		}
		r.URL.Path = "/"
		http.FileServer(files).ServeHTTP(w, r)
	})
}
func fatal(err error) { slog.Error("启动失败", "error", err); os.Exit(1) }
