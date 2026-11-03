// Package web is the front door of the browser build: it signs the owner in
// with a passkey (Face ID / Touch ID / Windows Hello) and passes signed-in
// requests on to the app, which listens on a loopback port of its own.
//
// There is one account. Every passkey registered belongs to it, so signing
// in on any device shows the same connections and workspace.
package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	sessionCookie   = "tablume_session"
	ceremonyCookie  = "tablume_ceremony"
	sessionLifetime = 30 * 24 * time.Hour // since last use
	ceremonyTTL     = 5 * time.Minute
	inviteTTL       = 15 * time.Minute
	touchEvery      = time.Hour
)

// ProxyHeader carries the secret that tells the app a request came through
// this front door (the app's own port is on loopback, but other local
// programs could reach it).
const ProxyHeader = "X-Tablume-Proxy"

type Config struct {
	// Public address as typed in the browser (https://db.example.com).
	URL     *url.URL
	DataDir string
	// The app's own server (http://127.0.0.1:port) and the header secret.
	Target      *url.URL
	ProxySecret string
	// Release closes the DB sessions of console tabs (a page was closed).
	Release func(ids []string)
	Logf    func(format string, args ...any)
}

type passkey struct {
	Name       string              `json:"name"`
	Created    time.Time           `json:"created"`
	LastUsed   time.Time           `json:"lastUsed"`
	Credential webauthn.Credential `json:"credential"`
}

type loginSession struct {
	Hash     string    `json:"hash"` // sha256 of the cookie value
	Passkey  string    `json:"passkey"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"lastUsed"`
}

type authState struct {
	UserID   []byte         `json:"userId"`
	Passkeys []passkey      `json:"passkeys"`
	Sessions []loginSession `json:"sessions"`
}

// webauthn.User for the one account.
type owner struct{ st *authState }

func (o owner) WebAuthnID() []byte          { return o.st.UserID }
func (o owner) WebAuthnName() string        { return "tablume" }
func (o owner) WebAuthnDisplayName() string { return "Tablume" }
func (o owner) WebAuthnCredentials() []webauthn.Credential {
	out := make([]webauthn.Credential, len(o.st.Passkeys))
	for i, p := range o.st.Passkeys {
		out[i] = p.Credential
	}
	return out
}

type ceremony struct {
	data     webauthn.SessionData
	register bool
	token    string // setup / invite token a registration was started with
	name     string // the new passkey's name
	expires  time.Time
}

type Server struct {
	cfg    Config
	wa     *webauthn.WebAuthn
	origin string
	secure bool

	mu         sync.Mutex
	st         authState
	ceremonies map[string]*ceremony
	setupToken string               // while no passkey exists
	invites    map[string]time.Time // one-time links for another device
}

func New(cfg Config) (*Server, error) {
	origin := cfg.URL.Scheme + "://" + cfg.URL.Host
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          cfg.URL.Hostname(),
		RPDisplayName: "Tablume",
		RPOrigins:     []string{origin},
	})
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:        cfg,
		wa:         wa,
		origin:     origin,
		secure:     cfg.URL.Scheme == "https",
		ceremonies: map[string]*ceremony{},
		invites:    map[string]time.Time{},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	if len(s.st.UserID) == 0 {
		s.st.UserID = randBytes(32)
		if err := s.save(); err != nil {
			return nil, err
		}
	}
	if len(s.st.Passkeys) == 0 {
		s.newSetupToken()
	}
	return s, nil
}

func (s *Server) path() string { return filepath.Join(s.cfg.DataDir, "auth.json") }

func (s *Server) load() error {
	b, err := os.ReadFile(s.path())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &s.st)
}

func (s *Server) save() error {
	b, err := json.MarshalIndent(&s.st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path())
}

// ResetPasskeys forgets every passkey and signs every device out; the
// server then prints a new setup link.
func ResetPasskeys(dataDir string) error {
	s := &Server{cfg: Config{DataDir: dataDir}}
	if err := s.load(); err != nil {
		return err
	}
	s.st.Passkeys = nil
	s.st.Sessions = nil
	return s.save()
}

// caller holds mu (or the server isn't serving yet)
func (s *Server) newSetupToken() {
	s.setupToken = randToken()
	s.logf("등록된 패스키가 없습니다. 이 주소를 열어 첫 패스키를 등록하세요 (서버를 다시 시작하면 바뀝니다):\n\n    %s/login?token=%s\n", s.origin, s.setupToken)
}

func (s *Server) logf(format string, args ...any) {
	if s.cfg.Logf != nil {
		s.cfg.Logf(format, args...)
	}
}

// ---------------------------------------------------------------- sessions

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// session returns the signed-in session of a request, or nil.
func (s *Server) session(w http.ResponseWriter, r *http.Request) *loginSession {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	h := hashToken(c.Value)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for i := range s.st.Sessions {
		ls := &s.st.Sessions[i]
		if subtle.ConstantTimeCompare([]byte(ls.Hash), []byte(h)) != 1 {
			continue
		}
		if now.Sub(ls.LastUsed) > sessionLifetime {
			s.st.Sessions = append(s.st.Sessions[:i], s.st.Sessions[i+1:]...)
			_ = s.save()
			return nil
		}
		if now.Sub(ls.LastUsed) > touchEvery {
			ls.LastUsed = now
			_ = s.save()
			if w != nil {
				s.setSessionCookie(w, c.Value) // slide the cookie's expiry too
			}
		}
		cp := *ls
		return &cp
	}
	return nil
}

// caller holds mu
func (s *Server) startSession(w http.ResponseWriter, credID []byte) {
	tok := randToken()
	now := time.Now()
	// drop expired ones while here
	kept := s.st.Sessions[:0]
	for _, ls := range s.st.Sessions {
		if now.Sub(ls.LastUsed) <= sessionLifetime {
			kept = append(kept, ls)
		}
	}
	s.st.Sessions = append(kept, loginSession{Hash: hashToken(tok), Passkey: b64(credID), Created: now, LastUsed: now})
	_ = s.save()
	s.setSessionCookie(w, tok)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    tok,
		Path:     "/",
		MaxAge:   int(sessionLifetime.Seconds()),
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		h := hashToken(c.Value)
		s.mu.Lock()
		for i, ls := range s.st.Sessions {
			if ls.Hash == h {
				s.st.Sessions = append(s.st.Sessions[:i], s.st.Sessions[i+1:]...)
				_ = s.save()
				break
			}
		}
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteLaxMode})
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- ceremonies

func (s *Server) putCeremony(w http.ResponseWriter, c *ceremony) {
	id := randToken()
	now := time.Now()
	c.expires = now.Add(ceremonyTTL)
	for k, v := range s.ceremonies {
		if now.After(v.expires) {
			delete(s.ceremonies, k)
		}
	}
	s.ceremonies[id] = c
	http.SetCookie(w, &http.Cookie{
		Name: ceremonyCookie, Value: id, Path: "/auth/", MaxAge: int(ceremonyTTL.Seconds()),
		HttpOnly: true, Secure: s.secure, SameSite: http.SameSiteStrictMode,
	})
}

// takeCeremony removes and returns the request's pending ceremony.
func (s *Server) takeCeremony(r *http.Request, register bool) *ceremony {
	ck, err := r.Cookie(ceremonyCookie)
	if err != nil {
		return nil
	}
	c := s.ceremonies[ck.Value]
	delete(s.ceremonies, ck.Value)
	if c == nil || c.register != register || time.Now().After(c.expires) {
		return nil
	}
	return c
}

// caller holds mu
func (s *Server) tokenValid(tok string) bool {
	if tok == "" {
		return false
	}
	if s.setupToken != "" && subtle.ConstantTimeCompare([]byte(tok), []byte(s.setupToken)) == 1 {
		return true
	}
	exp, ok := s.invites[tok]
	return ok && time.Now().Before(exp)
}

func (s *Server) loginBegin(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.st.Passkeys) == 0 {
		httpError(w, http.StatusConflict, "등록된 패스키가 없습니다. 서버 로그에 나온 등록 주소로 먼저 패스키를 등록하세요.")
		return
	}
	opts, data, err := s.wa.BeginLogin(owner{&s.st}, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.putCeremony(w, &ceremony{data: *data})
	writeJSON(w, opts)
}

func (s *Server) loginFinish(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.takeCeremony(r, false)
	if c == nil {
		httpError(w, http.StatusBadRequest, "로그인 요청이 만료되었습니다. 다시 시도하세요.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	cred, err := s.wa.FinishLogin(owner{&s.st}, c.data, r)
	if err != nil {
		httpError(w, http.StatusUnauthorized, "패스키를 확인하지 못했습니다: "+errDetail(err))
		return
	}
	if cred.Authenticator.CloneWarning {
		httpError(w, http.StatusUnauthorized, "패스키의 서명 카운터가 맞지 않습니다 (복제된 인증기일 수 있음).")
		return
	}
	for i := range s.st.Passkeys {
		if string(s.st.Passkeys[i].Credential.ID) == string(cred.ID) {
			s.st.Passkeys[i].Credential = *cred
			s.st.Passkeys[i].LastUsed = time.Now()
		}
	}
	s.startSession(w, cred.ID)
	writeJSON(w, map[string]bool{"ok": true})
}

type registerBeginReq struct {
	Token string `json:"token"`
	Name  string `json:"name"`
}

func (s *Server) registerBegin(w http.ResponseWriter, r *http.Request) {
	var req registerBeginReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "잘못된 요청입니다")
		return
	}
	signedIn := s.session(w, r) != nil
	s.mu.Lock()
	defer s.mu.Unlock()
	if !signedIn && !s.tokenValid(req.Token) {
		httpError(w, http.StatusForbidden, "등록 링크가 만료되었거나 올바르지 않습니다.")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "패스키"
	}
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	excl := make([]protocol.CredentialDescriptor, len(s.st.Passkeys))
	for i := range s.st.Passkeys {
		excl[i] = s.st.Passkeys[i].Credential.Descriptor()
	}
	req2 := true
	opts, data, err := s.wa.BeginRegistration(owner{&s.st},
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			RequireResidentKey: &req2,
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			UserVerification:   protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(excl),
	)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tok := ""
	if !signedIn {
		tok = req.Token
	}
	s.putCeremony(w, &ceremony{data: *data, register: true, token: tok, name: name})
	writeJSON(w, opts)
}

func (s *Server) registerFinish(w http.ResponseWriter, r *http.Request) {
	signedIn := s.session(w, r) != nil
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.takeCeremony(r, true)
	if c == nil {
		httpError(w, http.StatusBadRequest, "등록 요청이 만료되었습니다. 다시 시도하세요.")
		return
	}
	if !signedIn && !s.tokenValid(c.token) {
		httpError(w, http.StatusForbidden, "등록 링크가 만료되었거나 이미 사용되었습니다.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	cred, err := s.wa.FinishRegistration(owner{&s.st}, c.data, r)
	if err != nil {
		httpError(w, http.StatusBadRequest, "패스키를 등록하지 못했습니다: "+errDetail(err))
		return
	}
	now := time.Now()
	s.st.Passkeys = append(s.st.Passkeys, passkey{Name: c.name, Created: now, LastUsed: now, Credential: *cred})
	// the link is used up
	if c.token != "" {
		delete(s.invites, c.token)
		if c.token == s.setupToken {
			s.setupToken = ""
		}
	}
	s.setupToken = "" // a passkey exists now
	if err := s.save(); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logf("패스키를 등록했습니다: %s", c.name)
	if !signedIn {
		s.startSession(w, cred.ID)
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------- passkey list

type passkeyInfo struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"lastUsed"`
	Current  bool      `json:"current"` // this browser signed in with it
}

func (s *Server) listPasskeys(w http.ResponseWriter, r *http.Request, ls *loginSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]passkeyInfo, len(s.st.Passkeys))
	for i, p := range s.st.Passkeys {
		id := b64(p.Credential.ID)
		out[i] = passkeyInfo{ID: id, Name: p.Name, Created: p.Created, LastUsed: p.LastUsed, Current: id == ls.Passkey}
	}
	writeJSON(w, out)
}

func (s *Server) deletePasskey(w http.ResponseWriter, r *http.Request, ls *loginSession) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "잘못된 요청입니다")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.st.Passkeys) <= 1 {
		httpError(w, http.StatusConflict, "마지막 패스키는 지울 수 없습니다. 다른 패스키를 먼저 등록하세요.")
		return
	}
	for i, p := range s.st.Passkeys {
		if b64(p.Credential.ID) != req.ID {
			continue
		}
		s.st.Passkeys = append(s.st.Passkeys[:i], s.st.Passkeys[i+1:]...)
		// devices signed in with it are signed out
		kept := s.st.Sessions[:0]
		for _, x := range s.st.Sessions {
			if x.Passkey != req.ID {
				kept = append(kept, x)
			}
		}
		s.st.Sessions = kept
		if err := s.save(); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	httpError(w, http.StatusNotFound, "패스키가 없습니다")
}

func (s *Server) renamePasskey(w http.ResponseWriter, r *http.Request, ls *loginSession) {
	var req struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "잘못된 요청입니다")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.st.Passkeys {
		if b64(s.st.Passkeys[i].Credential.ID) == req.ID {
			name := []rune(strings.TrimSpace(req.Name))
			s.st.Passkeys[i].Name = string(name[:min(len(name), 60)])
			_ = s.save()
			writeJSON(w, map[string]bool{"ok": true})
			return
		}
	}
	httpError(w, http.StatusNotFound, "패스키가 없습니다")
}

// invite makes a one-time link that lets another device register a passkey
// of its own (for one that can't use the passkeys already registered).
func (s *Server) invite(w http.ResponseWriter, r *http.Request, ls *loginSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for k, exp := range s.invites {
		if now.After(exp) {
			delete(s.invites, k)
		}
	}
	tok := randToken()
	exp := now.Add(inviteTTL)
	s.invites[tok] = exp
	writeJSON(w, map[string]any{"url": s.origin + "/login?token=" + tok, "expires": exp})
}

// ---------------------------------------------------------------- helpers

func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func randToken() string { return b64(randBytes(24)) }

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func errDetail(err error) string {
	var pe *protocol.Error
	if errors.As(err, &pe) && pe.DevInfo != "" {
		return fmt.Sprintf("%s (%s)", pe.Details, pe.DevInfo)
	}
	return err.Error()
}
