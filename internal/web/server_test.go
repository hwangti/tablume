package web

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, backend http.Handler) (*Server, *httptest.Server) {
	t.Helper()
	app := httptest.NewServer(backend)
	t.Cleanup(app.Close)
	target, _ := url.Parse(app.URL)
	pub, _ := url.Parse("https://db.example.com")
	s, err := New(Config{URL: pub, DataDir: t.TempDir(), Target: target, ProxySecret: "sekret"})
	if err != nil {
		t.Fatal(err)
	}
	return s, app
}

func TestSignedOutRequestsStopAtTheDoor(t *testing.T) {
	reached := false
	s, _ := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	h := s.Handler()

	// a page load goes to the sign-in page
	r := httptest.NewRequest("GET", "https://db.example.com/", nil)
	r.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("page load: %d %q", w.Code, w.Header().Get("Location"))
	}
	// an API call gets 401
	r = httptest.NewRequest("POST", "https://db.example.com/wails/runtime", strings.NewReader("{}"))
	r.Header.Set("Origin", "https://db.example.com")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("runtime: %d", w.Code)
	}
	// a made-up cookie is no session
	r = httptest.NewRequest("GET", "https://db.example.com/assets/x.js", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "guess"})
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized || reached {
		t.Fatalf("bad cookie: %d reached=%v", w.Code, reached)
	}
}

func TestSignedInRequestsAreProxied(t *testing.T) {
	var got *http.Request
	s, _ := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		w.Write([]byte("app"))
	}))
	h := s.Handler()

	// sign in (as a passkey login would)
	w := httptest.NewRecorder()
	s.mu.Lock()
	s.startSession(w, []byte("cred"))
	s.mu.Unlock()
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("cookie flags: %+v", cookie)
	}

	r := httptest.NewRequest("POST", "https://db.example.com/wails/runtime", strings.NewReader("{}"))
	r.Header.Set("Origin", "https://db.example.com")
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "app" {
		t.Fatalf("proxied: %d %q", w.Code, w.Body.String())
	}
	if got.Header.Get(ProxyHeader) != "sekret" || got.Header.Get("Origin") != "" || got.Header.Get("Cookie") != "" {
		t.Fatalf("forwarded headers: %v", got.Header)
	}

	// another site can't make the browser call in, even with the cookie
	r = httptest.NewRequest("POST", "https://db.example.com/wails/runtime", strings.NewReader("{}"))
	r.Header.Set("Origin", "https://evil.example")
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	got = nil
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || got != nil {
		t.Fatalf("cross-origin: %d", w.Code)
	}
	// nor open the event WebSocket
	r = httptest.NewRequest("GET", "https://db.example.com/wails/events", nil)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || got != nil {
		t.Fatalf("cross-site websocket: %d", w.Code)
	}

	// signing out ends the session
	r = httptest.NewRequest("POST", "https://db.example.com/auth/logout", nil)
	r.Header.Set("Origin", "https://db.example.com")
	r.AddCookie(cookie)
	h.ServeHTTP(httptest.NewRecorder(), r)
	r = httptest.NewRequest("GET", "https://db.example.com/x", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", w.Code)
	}
}

func TestRegistrationNeedsALink(t *testing.T) {
	s, _ := newTestServer(t, http.NotFoundHandler())
	h := s.Handler()
	post := func(body string) int {
		r := httptest.NewRequest("POST", "https://db.example.com/auth/register/begin", strings.NewReader(body))
		r.Header.Set("Origin", "https://db.example.com")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if c := post(`{"token":"wrong"}`); c != http.StatusForbidden {
		t.Fatalf("wrong token: %d", c)
	}
	if c := post(`{"token":"` + s.setupToken + `"}`); c != 200 {
		t.Fatalf("setup token: %d", c)
	}
}
