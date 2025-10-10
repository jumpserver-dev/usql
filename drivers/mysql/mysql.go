package mysql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"github.com/xo/dburl"
	"io"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/go-sql-driver/mysql" // DRIVER
	"github.com/xo/usql/drivers"
	"github.com/xo/usql/drivers/metadata"
	mymeta "github.com/xo/usql/drivers/metadata/mysql"
	uu "net/url"
)

func CleanDSNUser(dsn string) (string, string, string) {
	colonIdx := strings.Index(dsn, ":")
	if colonIdx == -1 {
		origUser := dsn
		cleanUser := strings.ReplaceAll(origUser, "_", "")
		return origUser, cleanUser, cleanUser
	}

	origUser := dsn[:colonIdx]
	cleanUser := strings.ReplaceAll(origUser, "_", "")
	rest := dsn[colonIdx:]

	cleanedDSN := cleanUser + rest
	return origUser, cleanUser, cleanedDSN
}

func init() {
	d := drivers.Driver{
		AllowMultilineComments: true,
		AllowHashComments:      true,
		LexerName:              "mysql",
		UseColumnTypes:         true,
		ForceParams: drivers.ForceQueryParameters([]string{
			"parseTime", "true",
			"loc", "Local",
		}),
		Err: func(err error) (string, string) {
			if e, ok := err.(*mysql.MySQLError); ok {
				return strconv.Itoa(int(e.Number)), e.Message
			}
			return "", err.Error()
		},
		IsPasswordErr: func(err error) bool {
			if e, ok := err.(*mysql.MySQLError); ok {
				return e.Number == 1045
			}
			return false
		},
		NewMetadataReader: mymeta.NewReader,
		NewMetadataWriter: func(db drivers.DB, w io.Writer, opts ...metadata.ReaderOption) metadata.Writer {
			return metadata.NewDefaultWriter(mymeta.NewReader(db, opts...))(db, w)
		},
		Copy:         drivers.CopyWithInsert(func(int) string { return "?" }),
		NewCompleter: mymeta.NewCompleter,
	}

	d.Open = func(ctx context.Context, url *dburl.URL, f func() io.Writer, f2 func() io.Writer) (func(string, string) (*sql.DB, error), error) {
		return func(_, dsn string) (*sql.DB, error) {
			dsn, _ = uu.PathUnescape(dsn)
			username, newUsername, newDsn := CleanDSNUser(dsn)
			parsedURL, err := url.Parse(newDsn)
			if err != nil {
				return nil, err
			}

			parsedURL.RawQuery = parsedURL.RawQuery + parsedURL.RawFragment
			queryParams := parsedURL.Query()
			// If the host is an IPv6 address, it needs to be enclosed in square brackets
			if hostIp, err1 := netip.ParseAddr(url.Hostname()); err1 == nil && hostIp.Is6() {
				dsn = strings.Replace(dsn, url.Hostname(), fmt.Sprintf("[%s]", url.Hostname()), 1)
			}
			if queryParams.Get("tls") == "custom" {

				sslCA := queryParams.Get("ssl-ca")
				sslCert := queryParams.Get("ssl-cert")
				sslKey := queryParams.Get("ssl-key")

				//if sslCA == "" || sslCert == "" || sslKey == "" {
				//	return nil, fmt.Errorf("missing required ssl-ca, ssl-cert, or ssl-key parameter")
				//}

				queryParams.Del("ssl-ca")
				queryParams.Del("ssl-cert")
				queryParams.Del("ssl-key")

				rootCertPool := x509.NewCertPool()

				tlsConfig := &tls.Config{
					InsecureSkipVerify: true,
				}

				if sslCA != "" {
					pem, err := os.ReadFile(sslCA)
					if err != nil {
						return nil, fmt.Errorf("failed to read CA cert: %v", err)
					}
					if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
						return nil, fmt.Errorf("failed to append CA cert to pool")
					}
					tlsConfig.RootCAs = rootCertPool
				}

				if sslCert != "" && sslKey != "" {
					certs, err := tls.LoadX509KeyPair(sslCert, sslKey)
					if err != nil {
						return nil, fmt.Errorf("failed to load client cert and key: %v", err)
					}
					tlsConfig.Certificates = []tls.Certificate{certs}
				}
				err = mysql.RegisterTLSConfig("custom", tlsConfig)
				if err != nil {
					return nil, fmt.Errorf("failed to register custom TLS config: %v", err)
				}
			}

			parsedURL.RawQuery = queryParams.Encode()
			dsn = parsedURL.String()
			dsn = strings.Replace(dsn, newUsername, username, -1)

			return sql.Open("mysql", dsn)
		}, nil
	}
	drivers.Register("mysql", d, "memsql", "vitess", "tidb")
}
