package main

import (
	"context"
	"embed"
	"errors"
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

func run() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "3001"
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return errors.New("PORT harus berupa angka 1–65535")
	}
	frontend, err := fs.Sub(frontendFiles, "frontend/build")
	if err != nil {
		return errors.New("aset frontend tidak dapat dimuat")
	}
	listener, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", number))
	if err != nil {
		return fmt.Errorf("port lokal %d tidak dapat dibuka", number)
	}
	defer listener.Close()

	app, err := musehood.New(musehood.Options{
		DataDir: os.Getenv("DATA_DIR"), Frontend: frontend, TrustedHosts: os.Getenv("TRUSTED_HOSTS"),
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
	log.Printf("Musehood Go berjalan lokal di http://%s", listener.Addr())
	select {
	case err := <-serveDone:
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("listener lokal berhenti")
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
