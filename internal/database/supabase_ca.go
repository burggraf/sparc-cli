package database

import (
	"crypto/x509"
	_ "embed"
)

// supabaseRootCA is the public Supabase production database CA, not a secret.
// It is distributed with SPARC so TLS verification does not depend on the OS
// trusting Supabase's private database CA.
//
//go:embed prod-ca-2021.crt
var supabaseRootCA []byte

func supabaseRootCAPool() (*x509.CertPool, bool) {
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(supabaseRootCA) {
		return nil, false
	}
	return roots, true
}
