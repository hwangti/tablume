package dbx

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"tablume/internal/i18n"
	"time"

	"golang.org/x/crypto/ssh"
)

// tunnel forwards a local port (127.0.0.1:random) through an SSH server to
// the database host, so every driver works unchanged: the DSN just points
// at the local end. The SSH session is re-established on demand if it drops.
type tunnel struct {
	cfg    ConnConfig
	ln     net.Listener
	target string

	mu      sync.Mutex
	client  *ssh.Client
	hostKey string // SHA256 fingerprint seen on the first handshake
	closed  bool
}

func sshAddr(c ConnConfig) string {
	port := c.SSHPort
	if port == 0 {
		port = 22
	}
	return net.JoinHostPort(c.SSHHost, strconv.Itoa(port))
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[1:])
		}
	}
	return p
}

func (t *tunnel) authMethods() ([]ssh.AuthMethod, error) {
	c := t.cfg
	if c.SSHAuth != "key" {
		pw := c.SSHPassword
		return []ssh.AuthMethod{
			ssh.Password(pw),
			// Some servers only offer keyboard-interactive for passwords.
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				ans := make([]string, len(qs))
				for i := range ans {
					ans[i] = pw
				}
				return ans, nil
			}),
		}, nil
	}
	files := []string{c.SSHKeyFile}
	if strings.TrimSpace(c.SSHKeyFile) == "" {
		files = []string{"~/.ssh/id_ed25519", "~/.ssh/id_ecdsa", "~/.ssh/id_rsa"}
	}
	var signers []ssh.Signer
	var firstErr error
	for _, f := range files {
		b, err := os.ReadFile(expandHome(strings.TrimSpace(f)))
		if err != nil {
			if firstErr == nil && c.SSHKeyFile != "" {
				firstErr = i18n.Errorf("SSH 키 파일을 읽을 수 없습니다: %w", err)
			}
			continue
		}
		s, err := ssh.ParsePrivateKey(b)
		var missing *ssh.PassphraseMissingError
		if errors.As(err, &missing) {
			if c.SSHPassword == "" {
				return nil, i18n.Error("SSH 키에 암호가 걸려 있습니다. 키 암호를 입력하세요")
			}
			s, err = ssh.ParsePrivateKeyWithPassphrase(b, []byte(c.SSHPassword))
		}
		if err != nil {
			if firstErr == nil {
				firstErr = i18n.Errorf("SSH 키를 해석할 수 없습니다 (%s): %w", f, err)
			}
			continue
		}
		signers = append(signers, s)
	}
	if len(signers) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, i18n.Error("SSH 키 파일을 찾을 수 없습니다 (~/.ssh/id_ed25519, id_ecdsa, id_rsa)")
	}
	return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, nil
}

// dial connects to the SSH server. The host key is pinned: the first
// fingerprint is remembered (and saved with the connection by the caller),
// and a different key later is refused.
func (t *tunnel) dial() (*ssh.Client, error) {
	auth, err := t.authMethods()
	if err != nil {
		return nil, err
	}
	conf := &ssh.ClientConfig{
		User:    t.cfg.SSHUser,
		Auth:    auth,
		Timeout: 15 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fp := ssh.FingerprintSHA256(key)
			t.mu.Lock()
			defer t.mu.Unlock()
			want := t.cfg.SSHHostKey
			if want == "" {
				want = t.hostKey
			}
			if want != "" && want != fp {
				return i18n.Errorf("SSH 서버의 호스트 키가 저장된 것과 다릅니다 (저장: %s, 현재: %s). 서버를 바꾼 게 아니라면 중간자 공격일 수 있습니다. 맞다면 연결 편집에서 호스트 키를 초기화하세요", want, fp)
			}
			t.hostKey = fp
			return nil
		},
	}
	cl, err := ssh.Dial("tcp", sshAddr(t.cfg), conf)
	if err != nil {
		return nil, i18n.Errorf("SSH 연결 실패 (%s): %w", sshAddr(t.cfg), err)
	}
	go keepAlive(cl)
	return cl, nil
}

func keepAlive(cl *ssh.Client) {
	tk := time.NewTicker(30 * time.Second)
	defer tk.Stop()
	for range tk.C {
		if _, _, err := cl.SendRequest("keepalive@openssh.com", true, nil); err != nil {
			cl.Close()
			return
		}
	}
}

func (t *tunnel) remote() (net.Conn, error) {
	t.mu.Lock()
	cl := t.client
	t.mu.Unlock()
	if cl != nil {
		if c, err := cl.Dial("tcp", t.target); err == nil {
			return c, nil
		}
	}
	// The SSH session dropped (sleep, network change): reconnect once.
	cl, err := t.dial()
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		cl.Close()
		return nil, net.ErrClosed
	}
	if old := t.client; old != nil {
		old.Close()
	}
	t.client = cl
	t.mu.Unlock()
	return cl.Dial("tcp", t.target)
}

func openTunnel(cfg ConnConfig) (*tunnel, error) {
	if strings.TrimSpace(cfg.SSHHost) == "" {
		return nil, i18n.Error("SSH 호스트를 입력하세요")
	}
	host := cfg.Host
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	port := cfg.Port
	if port == 0 {
		port = defaultPort(cfg.Driver)
	}
	t := &tunnel{cfg: cfg, target: net.JoinHostPort(host, strconv.Itoa(port))}
	cl, err := t.dial()
	if err != nil {
		return nil, err
	}
	// Check the target is reachable from the SSH server before going on, for
	// a clear error instead of a driver timeout.
	probe, err := cl.Dial("tcp", t.target)
	if err != nil {
		cl.Close()
		return nil, i18n.Errorf("SSH 서버에서 DB(%s)에 연결할 수 없습니다: %w", t.target, err)
	}
	probe.Close()
	t.client = cl
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cl.Close()
		return nil, err
	}
	t.ln = ln
	go t.serve()
	return t, nil
}

func (t *tunnel) serve() {
	for {
		local, err := t.ln.Accept()
		if err != nil {
			return
		}
		go func() {
			remote, err := t.remote()
			if err != nil {
				local.Close()
				return
			}
			go func() {
				io.Copy(remote, local)
				remote.Close()
			}()
			io.Copy(local, remote)
			local.Close()
		}()
	}
}

func (t *tunnel) localPort() int { return t.ln.Addr().(*net.TCPAddr).Port }

func (t *tunnel) HostKey() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.hostKey
}

func (t *tunnel) Close() {
	t.mu.Lock()
	t.closed = true
	cl := t.client
	t.client = nil
	t.mu.Unlock()
	t.ln.Close()
	if cl != nil {
		cl.Close()
	}
}

func defaultPort(driver string) int {
	switch driver {
	case "postgres":
		return 5432
	case "mssql":
		return 1433
	case "oracle":
		return 1521
	}
	return 3306
}
