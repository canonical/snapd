// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

// Command snap-rehash creates OpenSSL-compatible SHA-1 hash links for
// certificate files in a directory. This is a separate binary from snapd
// because the snapd daemon operates in FIPS 140-3 mode (when enabled) which
// forbids use of the SHA-1 algorithm. This tool does not configure FIPS
// crypto and uses plain SHA-1 for the hash-based directory lookup that
// OpenSSL's c_rehash(1) would normally produce.
package main

import (
	"crypto/sha1"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/strutil"
)

var allowedSuffixes = []string{"pem", "crt", "cer"}

var certificatePEMBlockTypePattern = regexp.MustCompile(`^(X509 |TRUSTED |)?CERTIFICATE$`)

func isCertificatePEMBlockType(blockType string) bool {
	return certificatePEMBlockTypePattern.MatchString(blockType)
}

// sha1HashForCertFile computes the SHA-1 digest of all DER-encoded
// certificate blocks found in the file at path. For PEM files with
// multiple CERTIFICATE blocks, it hashes them all in order. For raw
// DER files, it hashes the single certificate.
func sha1HashForCertFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	h := sha1.New()

	if block, _ := pem.Decode(data); block != nil {
		// PEM-encoded: hash all CERTIFICATE blocks in order.
		rest := data
		found := false
		for {
			block, next := pem.Decode(rest)
			if block == nil {
				break
			}
			rest = next

			// TODO: block type 'X509 CRL' if we ever need this
			if !isCertificatePEMBlockType(block.Type) {
				continue
			}

			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return "", fmt.Errorf("cannot parse PEM certificate block: %v", err)
			}
			h.Write(cert.Raw)
			found = true
		}
		if !found {
			return "", fmt.Errorf("no CERTIFICATE PEM block found")
		}
	} else {
		// DER-encoded: single certificate.
		cert, err := x509.ParseCertificate(data)
		if err != nil {
			return "", fmt.Errorf("cannot parse DER certificate: %v", err)
		}
		h.Write(cert.Raw)
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// rehashDirectory creates SHA-1 hash links for all supported certificate files
// in dir, emulating the behaviour of OpenSSL's c_rehash(1). Each certificate file gets a
// hard link named <hash>.N where <hash> is the first 8 hex characters of
// the SHA-1 digest of the certificate chain's DER encoding and N is a
// collision suffix starting at 0.
func rehashDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("cannot read directory: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// TODO: we could support .crl files like openssl's c_rehash does,
		// but currently we have no .crl files in the bases
		extension := filepath.Ext(entry.Name())[1:]
		if !strutil.ListContains(allowedSuffixes, extension) {
			continue
		}

		// Skip the combined bundle file.
		if entry.Name() == "ca-certificates.crt" {
			continue
		}

		certPath := filepath.Join(dir, entry.Name())
		hash, err := sha1HashForCertFile(certPath)
		if err != nil {
			return fmt.Errorf("cannot hash certificate %q: %v", entry.Name(), err)
		}

		prefix := hash[:8]
		for suffix := 0; ; suffix++ {
			linkName := filepath.Join(dir, fmt.Sprintf("%s.%d", prefix, suffix))
			if err := os.Link(certPath, linkName); err != nil {
				if os.IsExist(err) {
					continue
				}
				return fmt.Errorf("cannot create hash link for %q: %v", entry.Name(), err)
			}
			break
		}
	}
	return nil
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: snap-rehash <directory>\n")
		os.Exit(1)
	}

	if err := rehashDirectory(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
