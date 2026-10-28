# JumpServer usql

## Db2 LUW

Db2 is compiled into the normal `CGO_ENABLED=0` binary on every supported target.
No IBM CLI, ODBC installation, native library bundle or KoKo dependency version
change is required to build this usql change.

The adapter uses the independent, MIT-licensed
[`github.com/go-db2/go-db2`](https://github.com/go-db2/go-db2) DRDA driver, pinned
to `v0.0.0-20261015010257-944faf6c05dc`. It is a community driver, not IBM's
official Go driver. This initial integration has been tested against Db2 LUW
11.5.7.0; it does not establish compatibility with Db2 for z/OS or IBM i.

```sh
CGO_ENABLED=0 go build -o build/usql .
build/usql --drivers
build/usql 'db2://USER:PERCENT_ENCODED_PASSWORD@HOST:50000/DATABASE' --no-password
```

The URL schemes are `db2` (alias `db`) and `db2s` (TLS required). Username and
password must be URL-encoded, including `@`, `:`, `/`, `?`, `#` and `%` when they
occur in a credential. The database name is required. The default TCP port is
50000, including for TLS; specify the server's actual TLS port explicitly.

Supported query options:

| Option | Meaning |
| --- | --- |
| `ssl=true` | Enable TLS 1.2 or later, validating the certificate and server name |
| `ssl-ca=/path/ca.pem` | Root CA file, with `ssl=true` |
| `ssl-cert=/path/client.pem`, `ssl-key=/path/client.key` | Paired client certificate/key files, with `ssl=true` |
| `ssl-server-name=db.example.com` | Expected TLS server name, useful with a tunnel |
| `timeout=30s` | Positive TCP dial timeout; this is not a SQL execution deadline |
| `secmec=3` or `secmec=9` | Requested DRDA authentication mechanism; the server can negotiate the mechanism |

Unsupported or duplicate query options fail explicitly. `db2s` cannot disable
TLS. There is no option to bypass certificate verification. A database name
over 18 bytes is rejected rather than silently truncated by the pinned driver.

SQL queries, positional `?` parameters and ordinary transactions use
`database/sql`. `VALUES`, `SELECT`, `WITH` and `TABLE` return tabular results.
The adapter also supplies Db2 `SYSCAT` metadata for `\dn`, `\dt`, `\dv` and
`\d`; use `S` to include system objects, for example `\dS SYSIBM.SYSDUMMY1`.
Unquoted metadata patterns are folded to uppercase; quoted names retain case.
Indexes, constraints, sequences and stored procedure metadata are not supplied
by this initial adapter. Existing `\c database` behavior remains PostgreSQL-only;
start a new usql process with another Db2 URL to change databases.

The pinned driver buffers query results in memory. Large-result export and
full stored procedure/LOB compatibility need separate validation; this change
does not claim streaming behavior or complete Db2 feature coverage.

## Validation

```sh
GOMAXPROCS=2 go test -p 2 ./...
GOMAXPROCS=2 go test -race -p 2 ./...
USQL_DB2_TEST_DSN_FILE=/path/to/private-dsn-file \
  GOMAXPROCS=2 CGO_ENABLED=0 go test -p 2 ./drivers/db2 -run TestLiveDB2 -v
```

The private file contains a Db2 URL and must remain outside this repository.
The opt-in integration test performs only read-only queries and rolls back its
read-only SQL transaction. It checks authentication, scalar values (including
Unicode and NULL), parameter binding and schema/table/column metadata. Normal
tests use local sockets for IPv4, IPv6, cancellation and TLS certificate checks.
TLS fixtures do not establish compatibility with a real TLS-enabled Db2 server.

Core's Db2 connection method and KoKo's protocol routing/release version are a
separate follow-up after this usql version is published.

The [Core SQL driver coverage review](docs/core-sql-driver-coverage.md) records
the nine current SQL database types, their actual driver packages, vendor versus
community provenance and the distinction between initialization and live tests.
