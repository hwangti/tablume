//go:build !no_mysql

package dbx

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"tablume/internal/i18n"

	"github.com/go-sql-driver/mysql"
)

// prepareParams turns the TLS options of a MySQL/MariaDB connection into a
// registered TLS config when the driver's own handling falls short:
//
//   - tls-ca=<file>: trust this CA (PEM). Go then verifies the chain itself
//     instead of the OS, whose rules macOS applies strictly ("certificate is
//     not standards compliant" for many self-made server certificates).
//   - tls=true through an SSH tunnel: the certificate must match the
//     database host, not the local end of the tunnel.
//
// Returns the params to use in the DSN.
func (mysqlDialect) prepareParams(c ConnConfig, viaTunnel bool) (string, error) {
	p := parseParams(c.Params)
	caFile := strings.TrimSpace(p.Get("tls-ca"))
	mode := strings.ToLower(p.Get("tls"))
	if caFile == "" && !(viaTunnel && mode == "true") {
		return c.Params, nil
	}
	cfg := &tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}
	if caFile != "" {
		b, err := os.ReadFile(expandHome(caFile))
		if err != nil {
			return "", i18n.Errorf("CA 인증서 파일을 읽을 수 없습니다: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(b) {
			return "", i18n.Error("CA 인증서 파일에서 PEM 인증서를 찾지 못했습니다")
		}
		cfg.RootCAs = pool
	}
	sum := sha256.Sum256([]byte(c.ID + "\x00" + c.Host + "\x00" + caFile))
	name := "tablume-" + hex.EncodeToString(sum[:8])
	if err := mysql.RegisterTLSConfig(name, cfg); err != nil {
		return "", err
	}
	p.Del("tls-ca")
	p.Set("tls", name)
	return p.Encode(), nil
}

// explainOpenError explains what to do when a MySQL/MariaDB server certificate
// isn't trusted.
func (mysqlDialect) explainOpenError(err error) error {
	if err == nil || !strings.Contains(err.Error(), "x509:") {
		return err
	}
	return fmt.Errorf("%w\n\n"+i18n.T("서버 인증서를 확인하지 못했습니다. 회사나 서버가 직접 만든 인증서라면 추가 옵션에 tls-ca=CA 인증서 파일 경로(.pem)를 넣으세요. 인증서 확인 없이 암호화만 하려면 tls=skip-verify 를 쓰세요"), err)
}
