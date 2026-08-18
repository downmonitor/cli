// Package target infers the kind of check from the target string.
//
// The scheme selects the check type, so there is no --type flag. Classifying
// the target locally means an unsupported target is reported as a usage error
// before any request is sent.
package target

import (
	"regexp"
	"strings"
)

// Kind is a type of check.
type Kind string

// Kinds of checks. Unknown is a target that matches none of them.
const (
	HTTP    Kind = "http"
	Port    Kind = "port"
	Ping    Kind = "ping"
	DNS     Kind = "dns"
	TLS     Kind = "tls"
	Unknown Kind = "unknown"
)

var hostPort = regexp.MustCompile(`^[A-Za-z0-9.\-]+:\d{1,5}$`)

// Of returns the kind of check a target asks for:
//
//	https://example.com/health  HTTP
//	db.example.com:5432         Port
//	ping://203.0.113.10         Ping
//	dns://example.com           DNS
//	tls://example.com           TLS
func Of(raw string) Kind {
	value := strings.TrimSpace(raw)
	lower := strings.ToLower(value)

	switch {
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		return HTTP
	case strings.HasPrefix(lower, "ping://"):
		return Ping
	case strings.HasPrefix(lower, "dns://"):
		return DNS
	case strings.HasPrefix(lower, "tls://"):
		return TLS
	case hostPort.MatchString(value):
		return Port
	}

	return Unknown
}

// Normalize trims the target and returns it with its kind. A bare host name
// is deliberately not guessed at: "example.com" could mean an HTTP, DNS or TLS
// check, and a wrong guess would bill a check nobody asked for.
func Normalize(raw string) (string, Kind) {
	value := strings.TrimSpace(raw)

	return value, Of(value)
}

// Served reports whether the API currently accepts this kind of check. Only
// HTTP is available for now.
func (k Kind) Served() bool {
	return k == HTTP
}

// Describe returns a short description used in error messages.
func (k Kind) Describe() string {
	switch k {
	case HTTP:
		return "an HTTP check"
	case Port:
		return "a TCP port check"
	case Ping:
		return "an ICMP ping check"
	case DNS:
		return "a DNS check"
	case TLS:
		return "a TLS handshake check"
	}

	return "a target we cannot read"
}
