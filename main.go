package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"html"
	"html/template"
	"log"
	"math/rand"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const (
	portMin        = 5001
	portMax        = 5999
	defaultTimeout = 10 * time.Minute
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "ww:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		portFlag      = flag.Int("port", 0, "port to listen on (default: random free port in 5001-5999)")
		listenFlag    = flag.String("listen", "localhost", "IP/host to listen on")
		urlHostFlag   = flag.String("url-host", "localhost", "host name shown in the printed URL")
		timeoutFlag   = flag.String("timeout", "", `idle shutdown after no retrievals (e.g. "10m", "1h 5m", "30s"); default 10m`)
		noTimeoutFlag = flag.Bool("no-timeout", false, "never shut down on idle (overrides -timeout)")
		dirFlag       = flag.String("dir", "", "directory to serve (default: current directory)")
	)
	flag.Parse()

	if *noTimeoutFlag && *timeoutFlag != "" {
		return errors.New("-no-timeout and -timeout are mutually exclusive")
	}

	// A zero timeout disables idle shutdown entirely.
	timeout := defaultTimeout
	switch {
	case *noTimeoutFlag:
		timeout = 0
	case *timeoutFlag != "":
		t, err := parseTimeout(*timeoutFlag)
		if err != nil {
			return err
		}
		timeout = t
	}

	// Resolve the serving root, following any symlinks so later
	// containment checks compare against the real path. Defaults to the
	// current directory; -dir overrides it.
	base := *dirFlag
	if base == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		base = cwd
	}
	root, err := filepath.EvalSymlinks(base)
	if err != nil {
		return fmt.Errorf("resolving served directory %q: %w", base, err)
	}
	if info, err := os.Stat(root); err != nil {
		return err
	} else if !info.IsDir() {
		return fmt.Errorf("not a directory: %q", base)
	}

	ln, port, err := listen(*listenFlag, *portFlag)
	if err != nil {
		return err
	}
	defer ln.Close()

	srv := &server{root: root}
	idle := newIdleTimer(timeout)

	httpSrv := &http.Server{
		Handler: logAccess(idle.wrap(srv)),
	}

	// Shut down on idle timeout or on an interrupt signal.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		select {
		case <-idle.fired:
			fmt.Fprintf(os.Stderr, "ww: idle for %s, shutting down\n", timeout)
		case <-ctx.Done():
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpSrv.Shutdown(shutdownCtx)
	}()

	fmt.Printf("Serving %s\n", root)
	fmt.Printf("  http://%s:%d/\n", *urlHostFlag, port)
	if timeout > 0 {
		fmt.Printf("  (quits after %s of inactivity)\n", timeout)
	}

	idle.start()
	if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// listen returns a TCP listener. If port is non-zero it binds that port;
// otherwise it picks a random free port in [portMin, portMax].
func listen(addr string, port int) (net.Listener, int, error) {
	if port != 0 {
		ln, err := net.Listen("tcp", net.JoinHostPort(addr, fmt.Sprintf("%d", port)))
		if err != nil {
			return nil, 0, fmt.Errorf("listening on %s port %d: %w", addr, port, err)
		}
		return ln, port, nil
	}

	// Try ports in a random order across the range.
	span := portMax - portMin + 1
	start := rand.Intn(span)
	for i := range span {
		p := portMin + (start+i)%span
		ln, err := net.Listen("tcp", net.JoinHostPort(addr, fmt.Sprintf("%d", p)))
		if err == nil {
			return ln, p, nil
		}
	}
	return nil, 0, fmt.Errorf("no free port available on %s in %d-%d", addr, portMin, portMax)
}

// idleTimer fires on its channel when no activity has occurred within the
// timeout. Each call to bump resets the countdown.
type idleTimer struct {
	timeout time.Duration
	timer   *time.Timer
	fired   chan struct{}
}

func newIdleTimer(timeout time.Duration) *idleTimer {
	return &idleTimer{
		timeout: timeout,
		fired:   make(chan struct{}),
	}
}

func (t *idleTimer) start() {
	if t.timeout <= 0 {
		return
	}
	t.timer = time.AfterFunc(t.timeout, func() {
		close(t.fired)
	})
}

func (t *idleTimer) bump() {
	if t.timer != nil {
		t.timer.Reset(t.timeout)
	}
}

// wrap returns a handler that bumps the idle timer on every request.
func (t *idleTimer) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.bump()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the response status code for access logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// logAccess wraps a handler, logging each request's method, path, status,
// and client address to stdout.
func logAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		log.Printf("%s %s %s -> %d", r.RemoteAddr, r.Method, r.URL.Path, rec.status)
	})
}

type server struct {
	root string
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Clean the URL path and join it onto the root.
	upath := r.URL.Path
	if !strings.HasPrefix(upath, "/") {
		upath = "/" + upath
	}
	cleaned := path.Clean(upath)
	target := filepath.Join(s.root, filepath.FromSlash(cleaned))

	// Resolve symlinks and verify the result stays within the root.
	// EvalSymlinks also fails for nonexistent paths, yielding a 404.
	real, err := filepath.EvalSymlinks(target)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !withinRoot(s.root, real) {
		http.Error(w, "403 forbidden: path is outside the served directory", http.StatusForbidden)
		return
	}

	info, err := os.Stat(real)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if info.IsDir() {
		s.serveDir(w, r, real, cleaned)
		return
	}
	http.ServeFile(w, r, real)
}

// withinRoot reports whether p is root or a descendant of root.
func withinRoot(root, p string) bool {
	if p == root {
		return true
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type dirEntry struct {
	Name     string
	URL      string
	IsDir    bool
	Size     int64
	Modified string // last-modified time, ISO 8601 local time
}

var indexTmpl = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Index of {{.Path}}</title>
<style>
body{font-family:system-ui,sans-serif;margin:2rem;}
h1{font-size:1.2rem;}
table{border-collapse:collapse;}
td{padding:0.15rem 1rem 0.15rem 0;}
.size{text-align:right;color:#666;}
.mod{color:#666;font-variant-numeric:tabular-nums;}
a{text-decoration:none;}
</style></head>
<body>
<h1>Index of {{.Path}}</h1>
<table>
{{if .Parent}}<tr><td><a href="{{.Parent}}">../</a></td><td class="size"></td><td class="mod"></td></tr>{{end}}
{{range .Entries}}<tr><td><a href="{{.URL}}">{{.Name}}{{if .IsDir}}/{{end}}</a></td><td class="size">{{if not .IsDir}}{{.Size}}{{end}}</td><td class="mod">{{.Modified}}</td></tr>
{{end}}</table>
</body>
</html>
`))

func (s *server) serveDir(w http.ResponseWriter, r *http.Request, dir, urlPath string) {
	// Redirect directory URLs to a trailing slash so relative links work.
	if !strings.HasSuffix(r.URL.Path, "/") {
		http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
		return
	}

	// If the directory contains an index.html, serve it instead of a listing.
	// Resolve symlinks and re-check containment, matching ServeHTTP's model.
	if real, err := filepath.EvalSymlinks(filepath.Join(dir, "index.html")); err == nil && withinRoot(s.root, real) {
		if info, err := os.Stat(real); err == nil && !info.IsDir() {
			http.ServeFile(w, r, real)
			return
		}
	}

	f, err := os.Open(dir)
	if err != nil {
		http.Error(w, "500 internal error", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	names, err := f.Readdirnames(-1)
	if err != nil {
		http.Error(w, "500 internal error", http.StatusInternalServerError)
		return
	}
	sort.Strings(names)

	entries := make([]dirEntry, 0, len(names))
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			continue // skip broken/inaccessible entries
		}
		link := (&neturl.URL{Path: name}).String()
		if info.IsDir() {
			link += "/"
		}
		entries = append(entries, dirEntry{
			Name:     name,
			URL:      link,
			IsDir:    info.IsDir(),
			Size:     info.Size(),
			Modified: info.ModTime().Local().Format(time.RFC3339),
		})
	}

	var parent string
	if urlPath != "/" {
		parent = path.Dir(strings.TrimSuffix(urlPath, "/"))
		if !strings.HasSuffix(parent, "/") {
			parent += "/"
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	indexTmpl.Execute(w, struct {
		Path    string
		Parent  string
		Entries []dirEntry
	}{
		Path:    html.EscapeString(urlPath),
		Parent:  parent,
		Entries: entries,
	})
}
