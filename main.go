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

type fileSystem struct {
	http.FileSystem
}

func (fs fileSystem) Open(name string) (http.File, error) {
	file, err := fs.FileSystem.Open(name)
	if err != nil {
		if os.IsNotExist(err) && filepath.Ext(name) == "" {
			return fs.FileSystem.Open(name + ".html")
		}
		return nil, err
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	if stat.IsDir() {
		index := filepath.Join(name, "index.html")
		if _, err := fs.FileSystem.Open(index); os.IsNotExist(err) {
			file.Close()
			return nil, os.ErrNotExist
		}
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
	}

	handler := http.FileServer(fileSystem{http.Dir(root)})
	if !*quiet {
		handler = withLogging(handler)
	}

	server := http.Server{
		Addr:    net.JoinHostPort(*host, *port),
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

	url := "http://" + server.Addr
	if *host == "0.0.0.0" {
		if ip := getLocalIP(); ip != "" {
			url = "http://" + net.JoinHostPort(ip, *port)
		}
	}

	fmt.Printf("\nServer started at \033[4m%s\033[0m\n\n", url)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}

	<-idleConnsClosed
}
