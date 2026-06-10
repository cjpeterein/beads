package doltutil

import (
	"fmt"
	"time"

	mysql "github.com/go-sql-driver/mysql"
)

// defaultMaxAllowedPacket mirrors go-sql-driver/mysql's own default
// (mysql.NewConfig sets MaxAllowedPacket to 64 MiB). Struct-literal
// mysql.Config values do not get this default, so we set it explicitly to
// suppress the driver's per-connection "SELECT @@max_allowed_packet" probe.
const defaultMaxAllowedPacket = 64 << 20

// ServerDSN holds connection parameters for building a MySQL DSN to a Dolt server.
// All DSNs built with this struct set parseTime=true and multiStatements=true.
type ServerDSN struct {
	Socket   string // Unix domain socket path; when set, Net="unix" and Host/Port are ignored
	Host     string
	Port     int
	User     string
	Password string        //nolint:gosec // G117: MySQL DSN password field; required by the connection-string builder, not serialized as JSON
	Database string        // optional; empty connects without selecting a database
	Timeout  time.Duration // connect timeout; 0 defaults to 5s
	TLS      bool
}

// String builds the MySQL DSN string. Always sets parseTime=true,
// multiStatements=true, allowNativePasswords=true, and a connect timeout.
func (d ServerDSN) String() string {
	timeout := d.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	net := "tcp"
	addr := fmt.Sprintf("%s:%d", d.Host, d.Port)
	if d.Socket != "" {
		net = "unix"
		addr = d.Socket
	}

	cfg := mysql.Config{
		User:                 d.User,
		Passwd:               d.Password,
		Net:                  net,
		Addr:                 addr,
		DBName:               d.Database,
		ParseTime:            true,
		MultiStatements:      true,
		Timeout:              timeout,
		AllowNativePasswords: true,
		// Match the driver's own default (mysql.NewConfig sets this to
		// 64 MiB). A struct-literal mysql.Config leaves MaxAllowedPacket at
		// the zero value, which the driver treats as "unknown" and probes with
		// SELECT @@max_allowed_packet on every physical connection. bd opens
		// many short-lived connections per invocation, so that round-trip is a
		// fixed per-connection tax on the Dolt server. Setting the documented
		// default keeps FormatDSN from emitting the param and lets the driver
		// skip the probe (connector.go: MaxAllowedPacket > 0).
		MaxAllowedPacket: defaultMaxAllowedPacket,
	}
	if d.TLS {
		cfg.TLSConfig = "true"
	} else {
		// go-sql-driver/mysql v1.8+ defaults to tls=preferred when TLSConfig
		// is empty. Dolt servers without TLS reject preferred-mode negotiation
		// with "TLS requested but server does not support TLS". Explicitly
		// disable TLS so connections work against non-TLS Dolt instances.
		cfg.TLSConfig = "false"
	}

	return cfg.FormatDSN()
}
