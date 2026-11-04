//go:build server

package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"tablume/internal/dbx"
	"tablume/internal/i18n"
	"tablume/internal/store"
	"tablume/internal/web"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The browser build. The app itself (pages and bindings) listens on a
// loopback port only the front door reaches; the front door listens on
// -listen, signs the owner in with a passkey and passes requests on.

const platform = "web"

// Sessions of pages that went away without saying so are closed after this
// (open pages touch theirs every few minutes).
const sessionIdle = 30 * time.Minute

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func setupPlatform(svc *App, o *application.Options) func() {
	listen := flag.String("listen", env("TABLUME_LISTEN", "127.0.0.1:8080"), "address to listen on (env TABLUME_LISTEN)")
	public := flag.String("url", env("TABLUME_URL", ""), "address as typed in the browser, e.g. https://db.example.com (env TABLUME_URL; needed unless listening on localhost)")
	dataDir := flag.String("data", env("TABLUME_DATA", ""), "data directory: connections, workspace, passkeys (env TABLUME_DATA; default the user config directory)")
	files := flag.String("files", env("TABLUME_FILES", ""), "the only folder SQLite databases and SSH keys are read from (env TABLUME_FILES; default <data>/files)")
	reset := flag.Bool("reset-passkeys", false, "forget every passkey and sign every device out, then print a new setup link")
	flag.Parse()

	fail := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "tablume: "+format+"\n", args...)
		os.Exit(2)
	}

	if *dataDir != "" {
		store.DirOverride = *dataDir
	}
	store.NoKeyring = true
	// SQL can't reach files outside the files folder (ATTACH, VACUUM INTO)
	dbx.SQLiteSandbox = true
	loadLanguage()
	dir, err := store.Dir()
	if err != nil {
		fail("data directory: %v", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		fail("data directory: %v", err)
	}
	if *files == "" {
		*files = filepath.Join(dir, "files")
	}
	if err := os.MkdirAll(*files, 0o700); err != nil {
		fail("files folder: %v", err)
	}
	if svc.fileRoot, err = filepath.Abs(*files); err != nil {
		fail("files folder: %v", err)
	}
	if *reset {
		if err := web.ResetPasskeys(dir); err != nil {
			fail("reset passkeys: %v", err)
		}
		log.Print(i18n.T("패스키를 모두 지웠습니다."))
	}

	pub, err := publicURL(*listen, *public)
	if err != nil {
		fail("%v", err)
	}

	// The app's own port, picked free now.
	inner, err := freePort()
	if err != nil {
		fail("internal port: %v", err)
	}
	// Wails reads these over its options; the app must stay on loopback.
	os.Unsetenv("WAILS_SERVER_HOST")
	os.Unsetenv("WAILS_SERVER_PORT")
	o.Server = application.ServerOptions{
		Host: "127.0.0.1",
		Port: inner,
		// A long query is one binding call: don't cut it off.
		ReadTimeout:  time.Minute,
		WriteTimeout: 24 * time.Hour,
		IdleTimeout:  10 * time.Minute,
	}
	secret := randHex(32)
	prev := o.Assets.Middleware
	o.Assets.Middleware = func(next http.Handler) http.Handler {
		if prev != nil {
			next = prev(next)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get(web.ProxyHeader)), []byte(secret)) != 1 {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	front, err := web.New(web.Config{
		URL:         pub,
		DataDir:     dir,
		Target:      &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(inner))},
		ProxySecret: secret,
		Release: func(ids []string) {
			for _, id := range ids {
				svc.db.CloseSession(id)
			}
		},
		Logf: log.Printf,
	})
	if err != nil {
		fail("%v", err)
	}

	return func() {
		ln, err := net.Listen("tcp", *listen)
		if err != nil {
			fail("listen %s: %v", *listen, err)
		}
		log.Printf(i18n.T("Tablume 웹: %s (listening on %s, data %s, files %s)"), pub, *listen, dir, svc.fileRoot)
		srv := &http.Server{Handler: front.Handler(), ReadHeaderTimeout: 15 * time.Second}
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatal(err)
			}
		}()
		go func() {
			for range time.Tick(5 * time.Minute) {
				if n := svc.db.CloseIdleSessions(sessionIdle); n > 0 {
					log.Printf(i18n.T("닫힌 페이지의 DB 세션 %d개를 정리했습니다"), n)
				}
			}
		}()
	}
}

// publicURL is the address the browser uses: passkeys are bound to its host
// name. Without -url, only a localhost listener can guess it.
func publicURL(listen, public string) (*url.URL, error) {
	if public == "" {
		host, port, err := net.SplitHostPort(listen)
		if err != nil {
			return nil, fmt.Errorf("-listen %q: %v", listen, err)
		}
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, i18n.Error("-url (TABLUME_URL)을 지정하세요: 브라우저에서 여는 주소, 예) https://db.example.com — 패스키가 이 주소의 도메인에 묶입니다")
		}
		public = "http://localhost:" + port
	}
	u, err := url.Parse(public)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, i18n.Errorf("-url %q: http(s)://호스트 형식이어야 합니다", public)
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" {
		return nil, i18n.Errorf("-url %q: 패스키는 HTTPS(또는 localhost)에서만 쓸 수 있습니다", public)
	}
	return u, nil
}

func freePort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port, nil
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
