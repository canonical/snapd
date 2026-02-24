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
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

type CertificateData struct {
	Raw    *x509.Certificate
	Digest string
}

type certificate struct {
	Name     string
	Path     string
	RealPath string
	Digest   string
}

func digestHexForChain(chainDER [][]byte) string {
	h := sha256.New224()
	for _, der := range chainDER {
		// Hash the DER bytes as-is (in file order).
		_, _ = h.Write(der)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// ParseCertificateData parses certificate data and returns the first certificate,
// plus the full chain DER blobs (all CERTIFICATE PEM blocks, in order).
//
// For DER input, it returns a single-certificate chain.
func ParseCertificateData(certData []byte) (*CertificateData, error) {
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
				return nil, fmt.Errorf("failed to parse certificate PEM block: %v", err)
			}
			if first == nil {
				first = cert
			}
			chainDER = append(chainDER, cert.Raw)
		}
		if first == nil {
			return nil, fmt.Errorf("no certificate PEM block found")
		}
		return &CertificateData{
			Raw:    first,
			Digest: digestHexForChain(chainDER),
		}, nil
	}

	// The file is not PEM-encoded, so we try to parse it as DER.
	// We return a single-certificate chain in this case.
	cert, err := x509.ParseCertificate(certData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DER certificate: %v", err)
	}
	return &CertificateData{
		Raw:    cert,
		Digest: digestHexForChain([][]byte{cert.Raw}),
	}, nil
}

func trimCrtExtension(name string) string {
	if strings.HasSuffix(name, ".crt") {
		return strings.TrimSuffix(name, ".crt")
	}
	return name
}

// isBlocked checks whether the given certificate is blocked
// based on it's name (special names or its path not being a .crt), or
// based on its digest being in the list of blocked digests.
func isBlocked(cert certificate, blockedCertDigests []string) bool {
	// Special case for ca-certificates.crt
	if cert.Name == "ca-certificates.crt" {
		return true
	}

	// Check that the real underlying filepath to the certificate ends with .crt
	if !strings.HasSuffix(cert.RealPath, ".crt") {
		return true
	}
	return strutil.ListContains(blockedCertDigests, cert.Digest)
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
		certBytes, err := os.ReadFile(certRealPath)
		if err != nil {
			logger.Noticef("Failed to read certificate %q: %v", certRealPath, err)
			continue
		}

		cert, err := ParseCertificateData(certBytes)
		if err != nil {
			logger.Noticef("Failed to parse certificate %q: %v", certRealPath, err)
			continue
		}

		// If the file is not a symbolic link then Path and RealPath will be identical.
		certObject := certificate{
			Name:     trimCrtExtension(caFile.Name()),
			Path:     filepath.Join(certsPath, caFile.Name()),
			RealPath: certRealPath,
			Digest:   cert.Digest,
		}
		certsObjects = append(certsObjects, certObject)
	}
	return certsObjects, nil
}

// readDigests reads the names of all files in the given directory
// and returns them as a list of strings (with any .crt extension trimmed).
// It expects that the files in the directory are named by their digest.
func readDigests(dir string) ([]string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read directory %q: %v", dir, err)
	}

	// Certificates are expected to be named by their digest,
	// and we trim any .crt extension prior to returning them.
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
func generateCACertificates(certs *certificates, outputPath string) error {
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

	for _, cert := range certs.SystemCertificates {
		if digests[cert.Digest] || isBlocked(cert, certs.BlockedDigests) {
			continue
		}
		if err := copyOne(cert.RealPath); err != nil {
			return fmt.Errorf("cannot copy certificate %q: %v", cert.Name, err)
		}
		digests[cert.Digest] = true
	}

	for _, cert := range certs.AddedCertificates {
		if digests[cert.Digest] || isBlocked(cert, certs.BlockedDigests) {
			continue
		}
		if err := copyOne(cert.RealPath); err != nil {
			return fmt.Errorf("cannot copy extra certificate %q: %v", cert.Name, err)
		}
		digests[cert.Digest] = true
	}
	return nil
}

type certificates struct {
	SystemCertificates []certificate
	AddedCertificates  []certificate
	BlockedDigests     []string
}

// loadCertificates retrieves the system certificates, user added certificates
// and blocked certificate digests, and returns as a convenient structure.
func loadCertificates() (*certificates, error) {
	certs := &certificates{}

	// We will be using the certificates from the rootfs as a starting point,
	// meaning we need to go into /etc/ssl/certs/ and read
	// all the certificates from there.
	systemCerts, err := parseCertificates(dirs.SystemCertsDir)
	if err != nil {
		return nil, err
	}
	certs.SystemCertificates = systemCerts

	// If the added directory exists, parse it
	if exists, isDir, err := osutil.DirExists(filepath.Join(dirs.SnapdPKIV1Dir, "added")); err == nil && exists && isDir {
		addedDir := filepath.Join(dirs.SnapdPKIV1Dir, "added")
		a, err := parseCertificates(addedDir)
		if err != nil {
			return nil, err
		}

		certs.AddedCertificates = a
	}

	// If the blocked directory exists, read the digests
	if exists, isDir, err := osutil.DirExists(filepath.Join(dirs.SnapdPKIV1Dir, "blocked")); err == nil && exists && isDir {
		blockedDir := filepath.Join(dirs.SnapdPKIV1Dir, "blocked")
		b, err := readDigests(blockedDir)
		if err != nil {
			return nil, err
		}

		certs.BlockedDigests = b
	}

	return certs, nil
}

// ensureDirectories ensures that required directories for the certificate database exist:
//
// structure of pki/v1:
// /var/lib/snapd/pki/v1/added/<digest>.crt (symlink)
// /var/lib/snapd/pki/v1/blocked/<digest>.crt (symlink)
// /var/lib/snapd/pki/v1/merged/*.crt (symlinks)
// /var/lib/snapd/pki/v1/merged/ca-certificates.crt
// /var/lib/snapd/pki/v1/<digest>.crt
func ensureDirectories() error {
	dirsToEnsure := []string{
		filepath.Join(dirs.SnapdPKIV1Dir, "added"),
		filepath.Join(dirs.SnapdPKIV1Dir, "blocked"),
		filepath.Join(dirs.SnapdPKIV1Dir, "merged"),
	}
	for _, dir := range dirsToEnsure {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot create directory %q: %v", dir, err)
		}
	}
	return nil
}

var GenerateCertificateDatabase = GenerateCertificateDatabaseImpl

// GenerateCertificateDatabase generates the ca-certificates.crt based on the following
// folders:
// - /etc/ssl/certs/ (base certificates from the system)
// - /var/lib/snapd/pki/v1/added/ (user added certificates)
// - /var/lib/snapd/pki/v1/blocked/ (user blocked certificates)
//
// Inside the added/ and blocked/ folders, the certificates are expected to be
// named by their digest (sha256 hash of the certificate chain).
// - /var/lib/snapd/pki/v1/added/<digest>.crt
// - /var/lib/snapd/pki/v1/blocked/<digest>.crt
//
// The resulting ca-certificates.crt is written to
// /var/lib/snapd/pki/v1/merged/ca-certificates.crt
// If a previous version of the ca-certificates.crt exists, it is backed up to
// /var/lib/snapd/pki/v1/merged/ca-certificates.crt.old
func GenerateCertificateDatabaseImpl() error {
	// we create the added/blocked/merged directories if they don't exist here.
	if err := ensureDirectories(); err != nil {
		return err
	}

	// create a copy of the current certificates in the snapd pki v1 dir
	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	caCertificateDbPath := filepath.Join(mergedDir, "ca-certificates.crt")
	caCertificateDbBackupPath := caCertificateDbPath + ".old"

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

	// We will be using the certificates from the rootfs as a starting point,
	// meaning we need to go into /etc/ssl/certs/ and read
	// all the certificates from there.
	certs, err := loadCertificates()
	if err != nil {
		return err
	}

	// make sure we catch any error here and restore the backup
	err = generateCACertificates(certs, mergedDir)
	return err
}
