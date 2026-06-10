package main

import (
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	neturl "net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// newServer builds a server rooted at a fresh temp dir (symlinks resolved,
// matching run()).
func newServer(t *testing.T) (*server, string) {
	t.Helper()
	dir := t.TempDir()
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &server{root: root}, root
}

// get drives the handler with a decoded URL path (what the net/http server
// hands us after it decodes the request line) and returns status + body.
func get(s *server, urlPath string) (int, string) {
	req := httptest.NewRequest("GET", "http://example", nil)
	req.URL.Path = urlPath
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)
	body, _ := io.ReadAll(rr.Result().Body)
	return rr.Code, string(body)
}

func TestServeFileAndDir(t *testing.T) {
	s, root := newServer(t)
	mustWrite(t, filepath.Join(root, "file.txt"), "hello")
	mustMkdir(t, filepath.Join(root, "sub"))
	mustWrite(t, filepath.Join(root, "sub", "nested.txt"), "deep")

	if code, body := get(s, "/file.txt"); code != 200 || body != "hello" {
		t.Errorf("/file.txt = %d %q", code, body)
	}
	if code, body := get(s, "/sub/nested.txt"); code != 200 || body != "deep" {
		t.Errorf("/sub/nested.txt = %d %q", code, body)
	}
	if code, body := get(s, "/"); code != 200 || !strings.Contains(body, "file.txt") {
		t.Errorf("/ index = %d, missing file.txt", code)
	}
	// Index must show an ISO 8601 local-time last-modified stamp.
	if _, body := get(s, "/"); !iso8601RE.MatchString(body) {
		t.Errorf("/ index missing ISO 8601 modified timestamp")
	}
	if code, _ := get(s, "/missing"); code != 404 {
		t.Errorf("/missing = %d, want 404", code)
	}
}

// TestTraversal: no amount of ".." in the URL path may escape the root.
func TestTraversal(t *testing.T) {
	s, root := newServer(t)
	mustWrite(t, filepath.Join(root, "inside.txt"), "inside")
	mustMkdir(t, filepath.Join(root, "sub"))

	// Plant a secret in the PARENT of root; traversal must never reach it.
	parent := filepath.Dir(root)
	secret := filepath.Join(parent, "ww_secret_"+filepath.Base(root)+".txt")
	mustWrite(t, secret, "TOP SECRET")
	defer os.Remove(secret)
	secretName := filepath.Base(secret)

	traversals := []string{
		"/../" + secretName,
		"/../../" + secretName,
		"/sub/../../" + secretName,
		"/../../../../../../etc/passwd",
		"/..",
		"/../",
		"/sub/../../sub/../../" + secretName,
		"/./../" + secretName,
	}
	for _, tp := range traversals {
		code, body := get(s, tp)
		if strings.Contains(body, "TOP SECRET") {
			t.Errorf("traversal %q LEAKED secret (code %d)", tp, code)
		}
		if code == 200 && strings.Contains(body, "passwd") {
			t.Errorf("traversal %q may have served /etc/passwd", tp)
		}
		// Acceptable outcomes: 404 (cleaned to a nonexistent root-relative
		// path), 200 serving the root index, or 301 (trailing-slash
		// redirect that itself cleans back to root) — never the secret.
		if code != 200 && code != 404 && code != 403 && code != 301 {
			t.Errorf("traversal %q unexpected code %d", tp, code)
		}
	}
}

// TestSymlinkEscape: symlinks pointing outside root are refused; symlinks
// staying inside root are served.
func TestSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks unreliable on windows CI")
	}
	s, root := newServer(t)
	mustWrite(t, filepath.Join(root, "real.txt"), "real")
	mustMkdir(t, filepath.Join(root, "realdir"))
	mustWrite(t, filepath.Join(root, "realdir", "child.txt"), "child")

	// Outside target.
	outDir := t.TempDir()
	mustWrite(t, filepath.Join(outDir, "secret.txt"), "OUTSIDE SECRET")

	mustSymlink(t, filepath.Join(outDir, "secret.txt"), filepath.Join(root, "escape-file"))
	mustSymlink(t, outDir, filepath.Join(root, "escape-dir"))
	// In-root symlinks (both should be allowed).
	mustSymlink(t, filepath.Join(root, "real.txt"), filepath.Join(root, "ok-file"))
	mustSymlink(t, filepath.Join(root, "realdir"), filepath.Join(root, "ok-dir"))

	escapes := []string{
		"/escape-file",
		"/escape-dir/secret.txt",
		"/escape-dir/",
		"/escape-dir",
	}
	for _, p := range escapes {
		code, body := get(s, p)
		if strings.Contains(body, "OUTSIDE SECRET") {
			t.Errorf("symlink escape %q LEAKED (code %d)", p, code)
		}
		if code != 403 && code != 404 && code != 301 {
			t.Errorf("symlink escape %q = %d, want 403/404/301", p, code)
		}
	}

	if code, body := get(s, "/ok-file"); code != 200 || body != "real" {
		t.Errorf("in-root symlink file = %d %q, want 200 real", code, body)
	}
	if code, body := get(s, "/ok-dir/child.txt"); code != 200 || body != "child" {
		t.Errorf("in-root symlink dir = %d %q, want 200 child", code, body)
	}
}

// TestWithinRoot guards the prefix-sibling edge case.
func TestWithinRoot(t *testing.T) {
	sep := string(filepath.Separator)
	root := sep + "tmp" + sep + "foo"
	cases := []struct {
		p    string
		want bool
	}{
		{root, true},
		{root + sep + "a", true},
		{root + sep + "a" + sep + "b", true},
		{sep + "tmp" + sep + "foobar", false}, // sibling sharing a prefix
		{sep + "tmp", false},
		{sep + "etc" + sep + "passwd", false},
	}
	for _, c := range cases {
		if got := withinRoot(root, c.p); got != c.want {
			t.Errorf("withinRoot(%q, %q) = %v, want %v", root, c.p, got, c.want)
		}
	}
}

// trickyNames are valid POSIX filenames exercising HTML and URL escaping.
var trickyNames = []string{
	"with space.txt",
	"quote'.txt",
	"amp&.txt",
	"angle<>.txt",
	"hash#.txt",
	"question?.txt",
	"percent%.txt",
	"plus+.txt",
	"semi;colon.txt",
	"unicodé-名前.txt",
	"<img src=x onerror=alert(1)>.txt", // XSS payload (no slash; '/' is illegal in names)
	"\"doublequote\".txt",
	"back\\slash.txt",
	"tab\there.txt",
}

// TestNameEscaping: tricky names must be HTML-escaped in the index, linked
// with a correctly percent-encoded href, and fetchable back to their content.
func TestNameEscaping(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("many of these names are invalid on windows")
	}
	s, root := newServer(t)
	for _, name := range trickyNames {
		mustWrite(t, filepath.Join(root, name), "C:"+name)
	}

	code, body := get(s, "/")
	if code != 200 {
		t.Fatalf("index code = %d", code)
	}

	// No raw, unescaped HTML injection should survive in the page; the
	// escaped form must be present instead.
	if strings.Contains(body, "<img src=x onerror=alert(1)>") {
		t.Error("index contains UNESCAPED <img onerror> payload")
	}
	if !strings.Contains(body, "&lt;img src=x onerror=alert(1)&gt;") {
		t.Error("index missing HTML-escaped form of the XSS payload")
	}

	// Every tricky file must be reachable by the decoded path the server
	// receives after the net/http layer decodes the request line.
	for _, name := range trickyNames {
		fetchCode, fetchBody := get(s, "/"+name)
		if fetchCode != 200 || fetchBody != "C:"+name {
			t.Errorf("fetch %q = %d %q, want 200 %q", name, fetchCode, fetchBody, "C:"+name)
		}
	}
}

var hrefRE = regexp.MustCompile(`href="([^"]*)"`)

// iso8601RE matches an RFC3339 / ISO 8601 timestamp with a timezone offset.
var iso8601RE = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(Z|[+-]\d{2}:\d{2})`)

// TestNameEscapingEndToEnd runs a real HTTP server, parses the links the
// index actually emits, and follows them through a real client. This proves
// the generated hrefs round-trip back to the right file regardless of the
// exact HTML/URL escaping the template applies.
func TestNameEscapingEndToEnd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("many of these names are invalid on windows")
	}
	dir := t.TempDir()
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range trickyNames {
		mustWrite(t, filepath.Join(root, name), "C:"+name)
	}

	ts := httptest.NewServer(&server{root: root})
	defer ts.Close()

	// Fetch the index and extract its hrefs.
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	indexBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	base, _ := neturl.Parse(ts.URL + "/")
	got := map[string]bool{}
	for _, m := range hrefRE.FindAllStringSubmatch(string(indexBody), -1) {
		raw := html.UnescapeString(m[1]) // undo HTML-attribute escaping
		ref, err := neturl.Parse(raw)
		if err != nil {
			t.Errorf("unparseable href %q: %v", m[1], err)
			continue
		}
		full := base.ResolveReference(ref)
		r, err := http.Get(full.String())
		if err != nil {
			t.Errorf("GET %q: %v", full, err)
			continue
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode == 200 && strings.HasPrefix(string(b), "C:") {
			got[strings.TrimPrefix(string(b), "C:")] = true
		}
	}

	// Every tricky file must have been reached by following a real link.
	for _, name := range trickyNames {
		if !got[name] {
			t.Errorf("index link did not round-trip to file %q", name)
		}
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
