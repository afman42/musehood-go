package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"musehood-go/internal/musehood"
)

//go:embed all:frontend/build
var frontendFiles embed.FS

// version diisi saat build rilis lewat -ldflags "-X main.version=v1.5.0".
var version = "dev"

type config struct {
	host         string
	port         int
	dataDir      string
	trustedHosts string
	showVersion  bool
}

// parseConfig membaca flag CLI; flag mengalahkan env, env mengalahkan bawaan.
func parseConfig(args []string, getenv func(string) string) (config, error) {
	envOr := func(key, fallback string) string {
		if value := getenv(key); value != "" {
			return value
		}
		return fallback
	}
	defaultPort := 3001
	if raw := getenv("PORT"); raw != "" {
		number, err := strconv.Atoi(raw)
		if err != nil || number < 1 || number > 65535 {
			return config{}, errors.New("PORT harus berupa angka 1–65535")
		}
		defaultPort = number
	}

	flags := flag.NewFlagSet("musehood-go", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	host := flags.String("host", envOr("HOST", "127.0.0.1"), "host untuk mendengarkan (kosongkan untuk semua antarmuka)")
	port := flags.Int("port", defaultPort, "port untuk mendengarkan")
	dataDir := flags.String("data-dir", getenv("DATA_DIR"), "direktori data runtime (bawaan: direktori kerja)")
	trustedHosts := flags.String("trusted-hosts", getenv("TRUSTED_HOSTS"), "host tambahan yang dipercaya, dipisahkan koma")
	showVersion := flags.Bool("version", false, "tampilkan versi lalu keluar")
	if err := flags.Parse(args); err != nil {
		return config{}, err
	}
	if *port < 1 || *port > 65535 {
		return config{}, errors.New("port harus berupa angka 1–65535")
	}
	return config{
		host:         *host,
		port:         *port,
		dataDir:      *dataDir,
		trustedHosts: *trustedHosts,
		showVersion:  *showVersion,
	}, nil
}

func run() error {
	cfg, err := parseConfig(os.Args[1:], os.Getenv)
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if cfg.showVersion {
		fmt.Printf("musehood-go %s\n", version)
		return nil
	}

	frontend, err := fs.Sub(frontendFiles, "frontend/build")
	if err != nil {
		return errors.New("aset frontend tidak dapat dimuat")
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port)))
	if err != nil {
		return fmt.Errorf("alamat %s tidak dapat dibuka", net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port)))
	}
	defer listener.Close()

	app, err := musehood.New(musehood.Options{
		DataDir: cfg.dataDir, Frontend: frontend, TrustedHosts: cfg.trustedHosts,
	})
	if err != nil {
		return errors.New("periksa data.json, aset frontend, dan izin penyimpanan lokal")
	}
	defer app.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{Handler: app, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second}
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	log.Printf("Musehood Go berjalan di http://%s", listener.Addr())
	select {
	case err := <-serveDone:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("listener berhenti")
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			_ = server.Close()
		}
	}
	return nil
}

func main() {
	if err := run(); err != nil {
		log.Printf("Musehood Go gagal dimulai: %s", err)
		os.Exit(1)
	}
}
