package dbx

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// testSSHServer is a minimal in-process SSH server that accepts one
// password and one public key and supports direct-tcpip (port forwarding).
func testSSHServer(t *testing.T, password string, allowed ssh.PublicKey) (host string, port int, fp string) {
	t.Helper()
	_, hk, _ := ed25519.GenerateKey(rand.Reader)
	signer, err := ssh.NewSignerFromKey(hk)
	if err != nil {
		t.Fatal(err)
	}
	conf := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, pw []byte) (*ssh.Permissions, error) {
			if c.User() == "tun" && string(pw) == password {
				return nil, nil
			}
			return nil, io.EOF
		},
		PublicKeyCallback: func(c ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
			if allowed != nil && string(k.Marshal()) == string(allowed.Marshal()) {
				return nil, nil
			}
			return nil, io.EOF
		},
	}
	conf.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(nc, conf)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nch := range chans {
					if nch.ChannelType() != "direct-tcpip" {
						nch.Reject(ssh.UnknownChannelType, "")
						continue
					}
					d := nch.ExtraData()
					hl := binary.BigEndian.Uint32(d)
					h := string(d[4 : 4+hl])
					p := binary.BigEndian.Uint32(d[4+hl:])
					remote, err := net.Dial("tcp", net.JoinHostPort(h, strconv.Itoa(int(p))))
					if err != nil {
						nch.Reject(ssh.ConnectionFailed, err.Error())
						continue
					}
					ch, rq, err := nch.Accept()
					if err != nil {
						remote.Close()
						continue
					}
					go ssh.DiscardRequests(rq)
					go func() { io.Copy(ch, remote); ch.CloseWrite() }()
					go func() { io.Copy(remote, ch); remote.Close() }()
				}
			}()
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port, ssh.FingerprintSHA256(signer.PublicKey())
}

func echoServer(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func roundTrip(t *testing.T, tun *tunnel) {
	t.Helper()
	c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(tun.localPort()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("ping"))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("echo through tunnel: %q %v", buf, err)
	}
}

func TestSSHTunnel(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	sshPub, _ := ssh.NewPublicKey(pub)
	host, port, fp := testSSHServer(t, "secret", sshPub)
	target := echoServer(t)
	base := ConnConfig{Driver: "mysql", Host: "127.0.0.1", Port: target, SSH: true, SSHHost: host, SSHPort: port, SSHUser: "tun"}

	t.Run("password", func(t *testing.T) {
		c := base
		c.SSHAuth, c.SSHPassword = "password", "secret"
		tun, err := openTunnel(c)
		if err != nil {
			t.Fatal(err)
		}
		defer tun.Close()
		if tun.HostKey() != fp {
			t.Errorf("host key %q want %q", tun.HostKey(), fp)
		}
		roundTrip(t, tun)
		// the SSH session drops: the next connection re-dials
		tun.mu.Lock()
		tun.client.Close()
		tun.mu.Unlock()
		roundTrip(t, tun)
	})

	t.Run("wrong password", func(t *testing.T) {
		c := base
		c.SSHAuth, c.SSHPassword = "password", "nope"
		if _, err := openTunnel(c); err == nil {
			t.Fatal("expected auth failure")
		}
	})

	t.Run("encrypted key", func(t *testing.T) {
		blk, err := ssh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("pp"))
		if err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(t.TempDir(), "id")
		os.WriteFile(f, pem.EncodeToMemory(blk), 0o600)
		c := base
		c.SSHAuth, c.SSHKeyFile = "key", f
		if _, err := openTunnel(c); err == nil || !strings.Contains(err.Error(), "암호") {
			t.Fatalf("want passphrase error, got %v", err)
		}
		c.SSHPassword = "pp"
		tun, err := openTunnel(c)
		if err != nil {
			t.Fatal(err)
		}
		defer tun.Close()
		roundTrip(t, tun)
	})

	t.Run("host key mismatch", func(t *testing.T) {
		c := base
		c.SSHAuth, c.SSHPassword, c.SSHHostKey = "password", "secret", "SHA256:something-else"
		if _, err := openTunnel(c); err == nil || !strings.Contains(err.Error(), "호스트 키") {
			t.Fatalf("want host key error, got %v", err)
		}
		c.SSHHostKey = fp
		tun, err := openTunnel(c)
		if err != nil {
			t.Fatal(err)
		}
		tun.Close()
	})

	t.Run("unreachable target", func(t *testing.T) {
		c := base
		c.SSHAuth, c.SSHPassword, c.Port = "password", "secret", 1
		if _, err := openTunnel(c); err == nil || !strings.Contains(err.Error(), "DB") {
			t.Fatalf("want target error, got %v", err)
		}
	})

	// End to end with a real database when the docker containers run.
	if os.Getenv("TABLUME_IT") != "" {
		t.Run("mariadb", func(t *testing.T) {
			c := targets(t)["mariadb"].cfg
			c.ID, c.Port = "sshm", 3306
			c.SSH, c.SSHHost, c.SSHPort, c.SSHUser, c.SSHAuth, c.SSHPassword = true, host, port, "tun", "password", "secret"
			m := NewManager()
			v, err := m.Connect(c)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Disconnect(c.ID)
			res, err := m.Execute(ExecRequest{TabID: "s", ConnID: c.ID, SQL: "SELECT 1+1", Cursor: -1})
			if err != nil || res.Results[0].Rows[0][0] != "2" {
				t.Fatalf("%v %+v", err, res)
			}
			if m.SSHHostKey(c.ID) != fp {
				t.Error("host key not reported")
			}
			t.Log("via ssh:", v)
		})
	}
}
