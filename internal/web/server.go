package web

import (
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/http/httputil"
	"strings"
	"tablume/internal/i18n"
)

// Handler serves the sign-in pages and passes everything else, once signed
// in, to the app.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /auth/login/begin", s.loginBegin)
	mux.HandleFunc("POST /auth/login/finish", s.loginFinish)
	mux.HandleFunc("POST /auth/register/begin", s.registerBegin)
	mux.HandleFunc("POST /auth/register/finish", s.registerFinish)
	mux.HandleFunc("POST /auth/logout", s.logout)
	mux.HandleFunc("GET /passkeys", s.authed(s.passkeysPage))
	mux.HandleFunc("GET /auth/passkeys", s.authed(s.listPasskeys))
	mux.HandleFunc("POST /auth/passkeys/delete", s.authed(s.deletePasskey))
	mux.HandleFunc("POST /auth/passkeys/rename", s.authed(s.renamePasskey))
	mux.HandleFunc("POST /auth/invite", s.authed(s.invite))
	mux.HandleFunc("POST /web/release", s.authed(s.release))
	mux.HandleFunc("GET /auth/logo.svg", serveLogo)
	proxy := s.proxy()
	mux.HandleFunc("/", s.authed(func(w http.ResponseWriter, r *http.Request, _ *loginSession) { proxy.ServeHTTP(w, r) }))
	return s.guard(mux)
}

// guard turns away requests other sites make the browser send: anything
// that changes state, and WebSocket upgrades, must come from our own pages.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		unsafe := r.Method != http.MethodGet && r.Method != http.MethodHead
		if unsafe || strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			if !s.sameOrigin(r) {
				http.Error(w, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		return strings.EqualFold(o, s.origin)
	}
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
		return true
	}
	return false
}

// authed lets signed-in requests through. A page load is sent to the
// sign-in page; anything else gets 401.
func (s *Server) authed(fn func(http.ResponseWriter, *http.Request, *loginSession)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ls := s.session(w, r)
		if ls == nil {
			if r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html") {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "tablume: login required", http.StatusUnauthorized)
			return
		}
		fn(w, r, ls)
	}
}

func (s *Server) proxy() *httputil.ReverseProxy {
	target := s.cfg.Target
	return &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(target)
			pr.Out.Host = target.Host
			// Same-origin was checked in guard; the app sees its own host.
			pr.Out.Header.Del("Origin")
			pr.Out.Header.Del("Sec-Fetch-Site")
			pr.Out.Header.Del("Cookie")
			pr.Out.Header.Set(ProxyHeader, s.cfg.ProxySecret)
		},
		// results stream as they come
		FlushInterval: -1,
	}
}

// release closes the DB sessions of a closed page's tabs (sent with
// navigator.sendBeacon as the page goes away).
func (s *Server) release(w http.ResponseWriter, r *http.Request, _ *loginSession) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		httpError(w, http.StatusBadRequest, i18n.T("잘못된 요청입니다"))
		return
	}
	var ids []string
	if err := json.Unmarshal(b, &ids); err != nil {
		httpError(w, http.StatusBadRequest, i18n.T("잘못된 요청입니다"))
		return
	}
	if s.cfg.Release != nil && len(ids) > 0 {
		s.cfg.Release(ids)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- pages

type pageData struct {
	Token       string
	HasPasskeys bool
	SignedIn    bool
}

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	signedIn := s.session(w, r) != nil
	s.mu.Lock()
	has := len(s.st.Passkeys) > 0
	if tok != "" && !s.tokenValid(tok) {
		tok = "-" // shown as an expired link
	}
	s.mu.Unlock()
	if signedIn && tok == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.page(w, r, loginTmpl, pageData{Token: tok, HasPasskeys: has, SignedIn: signedIn})
}

func (s *Server) passkeysPage(w http.ResponseWriter, r *http.Request, _ *loginSession) {
	s.page(w, r, passkeysTmpl, pageData{})
}

// page shows a template in the app's language (or, until one is chosen
// in the app, the browser's).
func (s *Server) page(w http.ResponseWriter, r *http.Request, t *template.Template, d pageData) {
	lang := i18n.Lang()
	if !i18n.Chosen() {
		lang = i18n.FromAccept(r.Header.Get("Accept-Language"))
	}
	t, err := t.Clone()
	if err != nil {
		s.logf("page: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t.Funcs(pageFuncs(lang))
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	if err := t.Execute(w, d); err != nil {
		s.logf("page: %v", err)
	}
}
