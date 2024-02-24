package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

type fileServerFileSystem struct {
	http.FileSystem
}

func (fs fileServerFileSystem) Open(name string) (http.File, error) {
	file, err := fs.FileSystem.Open(name)

	if os.IsNotExist(err) && filepath.Ext(name) == "" {
		return fs.FileSystem.Open(name + ".html")
	}

	return file, err
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (lrw *loggingResponseWriter) WriteHeader(status int) {
	lrw.status = status
	lrw.ResponseWriter.WriteHeader(status)
}

func withLogging(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		lrw := &loggingResponseWriter{w, http.StatusOK}
		h.ServeHTTP(lrw, r)

		duration := float64(time.Since(start).Microseconds()) / 1000

		statusColor := "32m"
		if lrw.status >= 400 {
			statusColor = "31m"
		} else if lrw.status >= 300 {
			statusColor = "33m"
		}

		fmt.Printf(
			"\033[90m[%s]\033[0m \033[%s%d\033[0m %s \033[90m(%.2fms)\033[0m\n",
			time.Now().Format(time.TimeOnly), statusColor, lrw.status, r.URL.Path, duration,
		)
	}
}

func withCacheControl(h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	}
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if n, ok := addr.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil {
			return n.IP.String()
		}
	}

	return ""
}

func main() {
	host := flag.String("host", "localhost", "Host to listen on")
	port := flag.String("port", "8080", "Port to listen on")
	quiet := flag.Bool("quiet", false, "Disable logging")

	flag.Usage = func() {
		fmt.Println("\nUsage:\n  serve [flags] [directory]\n\nFlags:")
		flag.PrintDefaults()
		fmt.Println()
	}

	flag.Parse()
	args := flag.Args()

	root := "."
	if len(args) != 0 {
		root = args[0]
		if _, err := os.Stat(root); os.IsNotExist(err) {
			fmt.Println("Error: provided directory could not be found")
			os.Exit(1)
		}
	}

	addr := *host + ":" + *port
	url := "http://" + addr
	if *host == "0.0.0.0" {
		if ip := getLocalIP(); ip != "" {
			url = "http://" + ip + ":" + *port
		}
	}

	handler := withCacheControl(
		http.FileServer(fileServerFileSystem{http.Dir(root)}),
	)

	if !*quiet {
		handler = withLogging(handler)
	}

	server := http.Server{
		Addr:    addr,
		Handler: handler,
	}

	idleConnsClosed := make(chan struct{})

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint

		fmt.Printf("\n\nShutting down...\n\n")

		if err := server.Shutdown(context.Background()); err != nil {
			log.Println(err)
		}

		close(idleConnsClosed)
	}()

	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	fmt.Printf("\nServer started at \033[4m%s\033[0m\n\n", url)

	<-idleConnsClosed
}
