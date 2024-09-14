package mysql

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"github.com/xo/dburl"
	"io"
	"os"
	"strconv"

	"github.com/go-sql-driver/mysql" // DRIVER
	"github.com/xo/usql/drivers"
	"github.com/xo/usql/drivers/metadata"
	mymeta "github.com/xo/usql/drivers/metadata/mysql"
)

func init() {
	drivers.Register("mysql", drivers.Driver{
		AllowMultilineComments: true,
		AllowHashComments:      true,
		LexerName:              "mysql",
		UseColumnTypes:         true,
		ForceParams: drivers.ForceQueryParameters([]string{
			"parseTime", "true",
			"loc", "Local",
			"sql_mode", "ansi",
		}),
		Open: func(ctx context.Context, url *dburl.URL, f func() io.Writer, f2 func() io.Writer) (func(string, string) (*sql.DB, error), error) {
			return func(_, dsn string) (*sql.DB, error) {
				parsedURL, err := url.Parse(dsn)
				if err != nil {
					return nil, err
				}

				queryParams := parsedURL.Query()

				if queryParams.Get("tls") == "custom" {

					sslCA := queryParams.Get("ssl-ca")
					sslCert := queryParams.Get("ssl-cert")
					sslKey := queryParams.Get("ssl-key")

					if sslCA == "" || sslCert == "" || sslKey == "" {
						return nil, fmt.Errorf("missing required ssl-ca, ssl-cert, or ssl-key parameter")
					}

					queryParams.Del("ssl-ca")
					queryParams.Del("ssl-cert")
					queryParams.Del("ssl-key")

					parsedURL.RawQuery = queryParams.Encode()
					dsn = parsedURL.String()

					rootCertPool := x509.NewCertPool()
					pem, err := os.ReadFile(sslCA)
					if err != nil {
						return nil, fmt.Errorf("failed to read CA cert: %v", err)
					}
					if ok := rootCertPool.AppendCertsFromPEM(pem); !ok {
						return nil, fmt.Errorf("failed to append CA cert to pool")
					}

					certs, err := tls.LoadX509KeyPair(sslCert, sslKey)
					if err != nil {
						return nil, fmt.Errorf("failed to load client cert and key: %v", err)
					}

					tlsConfig := &tls.Config{
						RootCAs:      rootCertPool,
						Certificates: []tls.Certificate{certs},
					}

					err = mysql.RegisterTLSConfig("custom", tlsConfig)
					if err != nil {
						return nil, fmt.Errorf("failed to register custom TLS config: %v", err)
					}
				}
				return sql.Open("mysql", dsn)
			}, nil
		},
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
	}, "memsql", "vitess", "tidb")
}
