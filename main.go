package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	addr        = flag.String("l", "localhost:8080", "Specify the address to listen on in the form `host:port` or `port`")
	hiddenFiles = flag.Bool("a", false, "Serve all files, including hidden files")
	dirListings = flag.Bool("d", false, "Enable directory listings")
	quiet       = flag.Bool("q", false, "Disable logging")
)

type filteredDirFile struct {
	http.File
}

func (f filteredDirFile) Readdir(count int) ([]os.FileInfo, error) {
	files, err := f.File.Readdir(count)

	if *hiddenFiles {
		return files, err
	}

	filtered := []os.FileInfo{}
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), ".") {
			filtered = append(filtered, file)
		}
	}

	return filtered, err
}

type fileSystem struct {
	http.FileSystem
}

func (fs fileSystem) Open(path string) (http.File, error) {
	if !*hiddenFiles {
		for _, s := range strings.Split(path, "/") {
			if strings.HasPrefix(s, ".") {
				return nil, os.ErrPermission
			}
		}
	}

	file, err := fs.FileSystem.Open(path)
	if err != nil {
		if os.IsNotExist(err) && filepath.Ext(path) == "" {
			return fs.FileSystem.Open(path + ".html")
		}
		return nil, err
	}

	if *dirListings {
		return filteredDirFile{file}, nil
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}

	if stat.IsDir() {
		index := filepath.Join(path, "index.html")
		if _, err := fs.FileSystem.Open(index); os.IsNotExist(err) {
			file.Close()
			return nil, os.ErrNotExist
		}
	}

	return file, nil
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

func run(root string) error {
	handler := http.FileServer(fileSystem{http.Dir(root)})
	if !*quiet {
		handler = withLogging(handler)
	}

	host, port, err := net.SplitHostPort(*addr)
	if err != nil {
		if _, err2 := strconv.Atoi(*addr); err2 == nil {
			port = *addr
		} else {
			return err
		}
	}

	if host == "" {
		host = "localhost"
	}

	server := http.Server{
		Addr:    net.JoinHostPort(host, port),
		Handler: handler,
	}

	idleConnsClosed := make(chan struct{})
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt)
		<-sigint
		fmt.Printf("\n\nShutting down...\n\n")
		server.Shutdown(context.Background())
		close(idleConnsClosed)
	}()

	url := "http://" + server.Addr
	if host == "0.0.0.0" {
		if ip := getLocalIP(); ip != "" {
			url = "http://" + net.JoinHostPort(ip, port)
		}
	}

	fmt.Printf("\nServer started at \033[4m%s\033[0m\n\n", url)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	<-idleConnsClosed
	return nil
}

func main() {
	flag.Usage = func() {
		out := strings.Builder{}
		out.WriteString("\nUsage:\n  serve [flags] [root]\n\nFlags:\n")

		flag.VisitAll(func(f *flag.Flag) {
			out.WriteString("  -")
			out.WriteString(f.Name)
			out.WriteString("    ")
			out.WriteString(f.Usage)
			out.WriteString("\n")
		})

		fmt.Println(out.String())
	}

	flag.Parse()
	args := flag.Args()

	root := "."
	if len(args) != 0 {
		root = args[0]
	}

	if err := run(root); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
