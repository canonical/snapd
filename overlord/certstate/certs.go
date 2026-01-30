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

package certstate

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/strutil"
)

type certificate struct {
	Name     string
	Path     string
	RealPath string
	Digest   string
}

// parseCertificateChainData parses certificate data and returns the first certificate,
// plus the full chain DER blobs (all CERTIFICATE PEM blocks, in order).
//
// For DER input, it returns a single-certificate chain.
func parseCertificateChainData(certData []byte) (*x509.Certificate, [][]byte, error) {
	// Many distro-provided *.crt files are PEM-encoded, while x509.ParseCertificate
	// expects DER.
	if block, _ := pem.Decode(certData); block != nil {
		rest := certData
		var chainDER [][]byte
		var first *x509.Certificate
		for {
			block, next := pem.Decode(rest)
			if block == nil {
				break
			}
			rest = next
			if block.Type != "CERTIFICATE" {
				continue
			}

			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, nil, err
			}
			if first == nil {
				first = cert
			}
			chainDER = append(chainDER, cert.Raw)
		}
		if first == nil {
			return nil, nil, fmt.Errorf("no certificate PEM block found")
		}
		return first, chainDER, nil
	}

	cert, err := x509.ParseCertificate(certData)
	if err != nil {
		return nil, nil, err
	}
	return cert, [][]byte{cert.Raw}, nil
}

// parseCertificateData returns the first certificate found.
//
// Note: a .crt file may contain a certificate chain (multiple certificates in sequence).
func parseCertificateData(certData []byte) (*x509.Certificate, error) {
	cert, _, err := parseCertificateChainData(certData)
	return cert, err
}

func chainDigestHex(chainDER [][]byte) string {
	h := sha256.New224()
	for _, der := range chainDER {
		// Hash the DER bytes as-is (in file order).
		_, _ = h.Write(der)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func trimCrtExtension(name string) string {
	if strings.HasSuffix(name, ".crt") {
		return strings.TrimSuffix(name, ".crt")
	}
	return name
}

// isBlocked validates the name of the certificate. We only allow certificates
// with the correct suffix ".crt" except for the ca-certificates.crt,
// or if the name is in the blockedCerts lists.
func isBlocked(cert certificate, blockedCerts []string) bool {
	// Special case for ca-certificates.crt
	if cert.Name == "ca-certificates.crt" {
		return true
	}

	// Check that the real underlying filepath to the certificate ends with .crt
	if !strings.HasSuffix(cert.RealPath, ".crt") {
		return true
	}
	return strutil.ListContains(blockedCerts, cert.Name)
}

// parseCertificates retrieves a list of files in the directory path and returns
// them as objects with their name and real path (any symlinks will be evaluated). Each file object
// contains both the path of file, and the evaluated real path, which are identical if the file is
// not a symlink.
func parseCertificates(certsPath string) ([]certificate, error) {
	logger.Debugf("Reading certificates from %s", certsPath)

	certFiles, err := os.ReadDir(certsPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read certs directory: %v", err)
	}

	var certsObjects []certificate
	for _, caFile := range certFiles {
		if caFile.IsDir() || !strings.HasSuffix(caFile.Name(), ".crt") {
			continue
		}

		// When provided with certificate directories they may be symbolic links to
		// the actual certificate file.
		certRealPath := filepath.Join(certsPath, caFile.Name())
		if caFile.Type()&os.ModeSymlink != 0 {
			resolvedPath, err := filepath.EvalSymlinks(certRealPath)
			if err != nil {
				logger.Noticef("Failed to parse certificate %q: cannot resolve symbolic link: %v", certRealPath, err)
				continue
			}
			certRealPath = resolvedPath
		}

		// Load the crt file and calculate the digest of the certificate.
		certData, err := os.ReadFile(certRealPath)
		if err != nil {
			logger.Noticef("Failed to read certificate %q: %v", certRealPath, err)
			continue
		}

		_, chainDER, err := parseCertificateChainData(certData)
		if err != nil {
			logger.Noticef("Failed to parse certificate %q: %v", certRealPath, err)
			continue
		}

		// If the file is not a symbolic link then Path and RealPath will be identical.
		certObject := certificate{
			Name:     trimCrtExtension(caFile.Name()),
			Path:     filepath.Join(certsPath, caFile.Name()),
			RealPath: certRealPath,
			Digest:   chainDigestHex(chainDER),
		}
		certsObjects = append(certsObjects, certObject)
	}
	return certsObjects, nil
}

func readDigests(dir string) ([]string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read directory %q: %v", dir, err)
	}

	var digests []string
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := trimCrtExtension(f.Name())
		digests = append(digests, name)
	}
	return digests, nil
}

// generateCACertificates generates the ca-certificates.crt to the output path
// The ca-certificates.crt is a concatenation of all the certs in the
// output path.
func generateCACertificates(certs, extras []certificate, blocked []string, outputPath string) error {
	certsPath := filepath.Join(outputPath, "ca-certificates.crt")
	certsFile, err := os.Create(certsPath)
	if err != nil {
		return fmt.Errorf("cannot create ca-certificates.crt: %v", err)
	}
	defer certsFile.Close()

	copyOne := func(from string) error {
		inf, err := os.Open(from)
		if err != nil {
			return err
		}
		defer inf.Close()
		if _, err := io.Copy(certsFile, inf); err != nil {
			return err
		}
		return nil
	}

	// avoid adding digests twice
	digests := make(map[string]bool)

	for _, cert := range certs {
		if digests[cert.Digest] {
			continue
		}

		if isBlocked(cert, blocked) {
			continue
		}

		if err := copyOne(cert.RealPath); err != nil {
			return fmt.Errorf("cannot copy certificate %q: %v", cert.Name, err)
		}
		digests[cert.Digest] = true
	}

	for _, cert := range extras {
		if digests[cert.Digest] {
			continue
		}

		if isBlocked(cert, blocked) {
			continue
		}

		if err := copyOne(cert.RealPath); err != nil {
			return fmt.Errorf("cannot copy extra certificate %q: %v", cert.Name, err)
		}
		digests[cert.Digest] = true
	}
	return nil
}

func GenerateCertificateDatabase() error {
	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	if err := os.MkdirAll(mergedDir, 0o755); err != nil {
		return fmt.Errorf("cannot create merged certificates directory: %v", err)
	}

	// create a copy of the current certificates in the snapd pki v1 dir
	caCertificateDbPath := filepath.Join(mergedDir, "ca-certificates.crt")
	caCertificateDbBackupPath := caCertificateDbPath + ".bak"

	if err := os.Rename(caCertificateDbPath, caCertificateDbBackupPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot backup existing ca-certificates.crt: %v", err)
	}

	// make sure we restore it on error
	var err error
	defer func() {
		if err != nil {
			if restoreErr := os.Rename(caCertificateDbBackupPath, caCertificateDbPath); restoreErr != nil && !os.IsNotExist(restoreErr) {
				logger.Noticef("cannot restore backup of ca-certificates: %v", restoreErr)
			}
		}
	}()

	// structure of pki/v1:
	// /var/lib/snapd/pki/v1/added/<digest>.crt (symlink)
	// /var/lib/snapd/pki/v1/blocked/<digest>.crt (symlink)
	// /var/lib/snapd/pki/v1/merged/*.crt (symlinks)
	// /var/lib/snapd/pki/v1/merged/ca-certificates.crt
	// /var/lib/snapd/pki/v1/<digest>.crt

	// we create the added/blocked/merged directories if they don't exist here.

	// We will be using the certificates from the rootfs as a starting point,
	// meaning we need to go into /etc/ssl/certs/ and read
	// all the certificates from there.
	baseCertsDir := filepath.Join(dirs.GlobalRootDir, "etc", "ssl", "certs")
	certs, err := parseCertificates(baseCertsDir)
	if err != nil {
		return err
	}

	addedDir := filepath.Join(dirs.SnapdPKIV1Dir, "added")
	if err := os.MkdirAll(addedDir, 0o755); err != nil {
		return fmt.Errorf("cannot create added certificates directory: %v", err)
	}

	added, err := parseCertificates(addedDir)
	if err != nil {
		return err
	}

	blockedDir := filepath.Join(dirs.SnapdPKIV1Dir, "blocked")
	if err := os.MkdirAll(blockedDir, 0o755); err != nil {
		return fmt.Errorf("cannot create blocked certificates directory: %v", err)
	}

	blocked, err := readDigests(blockedDir)
	if err != nil {
		return err
	}

	// make sure we catch any error here and restore the backup
	err = generateCACertificates(certs, added, blocked, mergedDir)
	return err
}
