package cli

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
)

// networkCode names a failed request the way Node.js does (error.code), and says whether it was a timeout.
func networkCode(err error) (code string, timedOut bool) {
	var coded *codedError
	if errors.As(err, &coded) {
		return coded.code, false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return "ENOTFOUND", false
		}
		return "EAI_AGAIN", false
	}
	if code := tlsCode(err); code != "" {
		return code, false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if name := errnoName(errno); name != "" {
			return name, name == "ETIMEDOUT"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return "ETIMEDOUT", true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "ETIMEDOUT", true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "ECONNRESET", false // "socket hang up"
	}
	text := err.Error()
	switch {
	case strings.Contains(text, "server response headers exceeded"):
		return "HPE_HEADER_OVERFLOW", false
	case strings.Contains(text, "malformed HTTP"):
		return "HPE_INVALID_CONSTANT", false
	case strings.Contains(text, "connection reset"):
		return "ECONNRESET", false
	case strings.Contains(text, "broken pipe"):
		return "EPIPE", false
	}
	return "ERROR", false
}

// tlsCode names a certificate or TLS failure with OpenSSL's codes, as Node.js reports them.
func tlsCode(err error) string {
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return "ERR_TLS_CERT_ALTNAME_INVALID"
	}
	var invalid x509.CertificateInvalidError
	if errors.As(err, &invalid) {
		if invalid.Reason == x509.Expired {
			return "CERT_HAS_EXPIRED"
		}
		return "UNABLE_TO_VERIFY_LEAF_SIGNATURE"
	}
	var unknown x509.UnknownAuthorityError
	if errors.As(err, &unknown) {
		var chain []*x509.Certificate
		var verification *tls.CertificateVerificationError
		if errors.As(err, &verification) {
			chain = verification.UnverifiedCertificates
		}
		if len(chain) == 0 && unknown.Cert != nil {
			chain = []*x509.Certificate{unknown.Cert}
		}
		switch {
		case len(chain) == 1 && selfSigned(chain[0]):
			return "DEPTH_ZERO_SELF_SIGNED_CERT"
		case len(chain) > 1 && selfSigned(chain[len(chain)-1]):
			return "SELF_SIGNED_CERT_IN_CHAIN"
		case len(chain) > 1:
			return "UNABLE_TO_GET_ISSUER_CERT_LOCALLY"
		}
		return "UNABLE_TO_VERIFY_LEAF_SIGNATURE"
	}
	var record tls.RecordHeaderError
	if errors.As(err, &record) {
		return "EPROTO"
	}
	var alert tls.AlertError
	if errors.As(err, &alert) {
		return "EPROTO"
	}
	return ""
}

func selfSigned(c *x509.Certificate) bool {
	return bytes.Equal(c.RawIssuer, c.RawSubject) && c.CheckSignatureFrom(c) == nil
}
