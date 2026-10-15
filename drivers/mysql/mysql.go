package mysql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"github.com/xo/dburl"
	"io"
	"net"
	"net/netip"
	"os"
	"strconv"

	"github.com/go-sql-driver/mysql" // DRIVER
	"github.com/xo/usql/drivers"
	"github.com/xo/usql/drivers/metadata"
	mymeta "github.com/xo/usql/drivers/metadata/mysql"
)

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
			url.RawQuery = url.RawQuery + url.RawFragment
			queryParams := url.Query()

			if queryParams.Get("tls") == "custom" {

				sslCA := queryParams.Get("ssl-ca")
				sslCert := queryParams.Get("ssl-cert")
				sslKey := queryParams.Get("ssl-key")

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
				err := mysql.RegisterTLSConfig("custom", tlsConfig)
				if err != nil {
					return nil, fmt.Errorf("failed to register custom TLS config: %v", err)
				}
			}

			url.RawQuery = queryParams.Encode()
			newDSN, err := dburl.Parse(url.String())
			if err != nil {
				return nil, err
			}
			dsn = newDSN.DSN
			if hostIP, err := netip.ParseAddr(url.Hostname()); err == nil && hostIP.Is6() {
				// dburl omits IPv6 brackets; repair the final DSN after TLS options
				// have been processed so URL regeneration cannot discard the fix.
				cfg, err := mysql.ParseDSN(dsn)
				if err != nil {
					return nil, err
				}
				port := url.Port()
				if port == "" {
					port = "3306"
				}
				cfg.Addr = net.JoinHostPort(url.Hostname(), port)
				dsn = cfg.FormatDSN()
			}
			return sql.Open("mysql", dsn)
		}, nil
	}
	drivers.Register("mysql", d, "memsql", "vitess", "tidb")
}
