// Command pcsentinel runs the PC Sentinel monitoring agent and serves the
// dashboard on a local-only HTTP port.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/dirirahmed/pc-sentinel/internal/api"
	"github.com/dirirahmed/pc-sentinel/internal/collector"
	"github.com/dirirahmed/pc-sentinel/internal/config"
	"github.com/dirirahmed/pc-sentinel/internal/monitor"
	"github.com/dirirahmed/pc-sentinel/internal/storage"
	"github.com/dirirahmed/pc-sentinel/web"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "pcsentinel:", err)
		os.Exit(1)
	}
}

func run() error {
	dataDir := flag.String("data-dir", defaultDataDir(), "directory for config.json and the history database")
	configPath := flag.String("config", "", "config file path (default <data-dir>/config.json)")
	port := flag.Int("port", 0, "override the configured API port")
	openBrowser := flag.Bool("open", runtime.GOOS == "windows", "open the dashboard in the default browser on start")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if *configPath == "" {
		*configPath = filepath.Join(*dataDir, "config.json")
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		// A broken config file shouldn't stop monitoring; defaults are safe.
		log.Error("using default settings", "err", err)
	}
	if *port != 0 {
		cfg.Port = *port
		if err := cfg.Validate(); err != nil {
			return err
		}
	}
	cfgStore := config.NewStore(*configPath, cfg)

	// Bind before touching the database so a second instance fails fast
	// instead of sharing (and closing alerts in) the first one's history.
	addr := net.JoinHostPort(cfg.ListenAddress, strconv.Itoa(cfg.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s (is PC Sentinel already running?): %w", addr, err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var store monitor.Store
	dbPath := filepath.Join(*dataDir, "history.db")
	db, dbErr := storage.Open(dbPath)
	if dbErr != nil {
		log.Error("history database unavailable; continuing with live data only", "path", dbPath, "err", dbErr)
	} else {
		defer db.Close()
		store = db
		if n, err := db.CloseDanglingAlerts(ctx, time.Now()); err == nil && n > 0 {
			log.Info("closed alerts left open by previous run", "count", n)
		}
	}

	col := collector.New(ctx, log)
	defer col.Close()
	mon := monitor.New(monitor.Options{
		Config: cfgStore, Collector: col, Processes: collector.NewProcessSampler(),
		Store: store, StoreErr: dbErr, Host: collector.HostInfo(ctx), Logger: log,
	})

	static, bundled := web.Dist()
	if !bundled {
		log.Warn("frontend not bundled; API only (run `npm run build` in web/ before `go build` to embed it)")
	}
	srv := &http.Server{
		Handler:           api.New(mon, cfgStore, static, log).Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	done := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(done)
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server stopped", "err", err)
			stop()
		}
	}()

	url := "http://" + dashboardHost(cfg.ListenAddress) + ":" + strconv.Itoa(cfg.Port)
	log.Info("PC Sentinel running", "dashboard", url, "data", *dataDir)
	if *openBrowser && bundled {
		openURL(url)
	}

	<-ctx.Done()
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	<-done
	return nil
}

func defaultDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil {
		return filepath.Join(dir, "PCSentinel")
	}
	return "pcsentinel-data"
}

func dashboardHost(listen string) string {
	if listen == "0.0.0.0" || listen == "::" || listen == "" {
		return "localhost"
	}
	return listen
}

func openURL(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
