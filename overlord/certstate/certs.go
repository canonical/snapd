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
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

const (
	CertificateStateUnset    = "unset"
	CertificateStateAccepted = "accepted"
	CertificateStateBlocked  = "blocked"
)

type CertificateData struct {
	Raw             *x509.Certificate
	Sha256          string
	SubjectNameSha1 string
}

type certificate struct {
	Name            string
	Path            string
	RealPath        string
	Sha256          string
	SubjectNameSha1 string
}

// TODO: .crl not supported for now, and there is none of this type carried
// in the bases
var allowedSuffixes = []string{"pem", "crt", "cer"}

// certificatePEMBlockTypePattern matches the PEM labels accepted when scanning
// certificate files or bundles.
var certificatePEMBlockTypePattern = regexp.MustCompile(`^(X509 |TRUSTED |)?CERTIFICATE$`)

// sha256HexForChain returns the content fingerprint for a certificate payload.
// For PEM bundles, every certificate DER block contributes to the digest in
// file order so two files that share a leaf certificate but differ elsewhere do
// not collapse to the same value.
func sha256HexForChain(chainDER [][]byte) string {
	h := sha256.New224()
	for _, der := range chainDER {
		// Hash the DER bytes as-is (in file order).
		_, _ = h.Write(der)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

// sha1HexForCertSubjectName reproduces the OpenSSL subject-name hash used by
// c_rehash-style lookup links.
// OBS: This is not great, because generating SHA1 hashes is not allowed
// under the go FIPS toolchain. In the future this needs to somewhere else, and
// not stay here. The use-case here is covered by the 140-3 FIPS, as we don't use
// SHA1 for digital signage. But the problem is the go FIPS toolchain will throw
// a runtime error in all cases.
func sha1HexForCertSubjectName(cert *x509.Certificate) (string, error) {
	canonicalSubject, err := canonicalSubjectNameDER(cert.RawSubject)
	if err != nil {
		return "", err
	}

	// OpenSSL's X509_NAME_hash_ex uses SHA-1 over the canonicalized subject DN
	// and returns the first 4 bytes in little-endian order.
	digest := sha1.Sum(canonicalSubject)
	return fmt.Sprintf("%08x", binary.LittleEndian.Uint32(digest[:4])), nil
}

// decodePemBlocks extracts certificate PEM blocks from data, returning their
// DER payloads in file order together with the first parsed certificate.
func decodePemBlocks(data []byte) (blocks [][]byte, first *x509.Certificate, err error) {
	rest := data
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = next
		if !certificatePEMBlockTypePattern.MatchString(block.Type) {
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse certificate PEM block: %v", err)
		}
		if first == nil {
			first = cert
		}
		blocks = append(blocks, cert.Raw)
	}
	return blocks, first, nil
}

// ParseCertificateData parses a PEM or DER certificate payload and returns the
// first certificate together with the digests snapd tracks for it.
//
// For PEM bundles, the content digest covers every certificate block in file
// order. The subject-name hash is set only for single-certificate inputs,
// matching the hash-link behavior of c_rehash and openssl x509 -subject_hash.
func ParseCertificateData(certData []byte) (*CertificateData, error) {
	// Many distro-provided cert files are PEM-encoded, while x509.ParseCertificate
	// expects DER.
	if block, _ := pem.Decode(certData); block != nil {
		blocks, first, err := decodePemBlocks(certData)
		if err != nil {
			return nil, err
		}

		if len(blocks) == 0 {
			return nil, fmt.Errorf("no certificate PEM block found")
		}

		// only calculate the subject name hash if we have a single certificate
		// which is what openssl does.
		var subjectNameSha1 string
		if len(blocks) == 1 {
			hash, err := sha1HexForCertSubjectName(first)
			if err != nil {
				return nil, fmt.Errorf("failed to hash certificate subject name: %v", err)
			}
			subjectNameSha1 = hash
		}

		return &CertificateData{
			Raw:             first,
			Sha256:          sha256HexForChain(blocks),
			SubjectNameSha1: subjectNameSha1,
		}, nil
	}

	// The file is not PEM-encoded, so we try to parse it as DER.
	// We return a single-certificate chain in this case.
	cert, err := x509.ParseCertificate(certData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse DER certificate: %v", err)
	}

	subjectNameSha1, err := sha1HexForCertSubjectName(cert)
	if err != nil {
		return nil, fmt.Errorf("failed to hash certificate subject name: %v", err)
	}
	return &CertificateData{
		Raw:             cert,
		Sha256:          sha256HexForChain([][]byte{cert.Raw}),
		SubjectNameSha1: subjectNameSha1,
	}, nil
}

// trimExtension strips one supported certificate-file extension from name.
func trimExtension(name string) string {
	extension := filepath.Ext(name)
	if len(extension) != 0 && strutil.ListContains(allowedSuffixes, extension[1:]) {
		return strings.TrimSuffix(name, extension)
	}
	return name
}

// isBlocked checks whether the given certificate is blocked
// based on its name (special names or its path not being a supported extension), or
// based on its digest being in the list of blocked digests.
func isBlocked(cert certificate, blockedCertDigests []string) bool {
	// Special case for ca-certificates.crt
	if cert.Name == "ca-certificates.crt" {
		return true
	}

	// Check that the real underlying filepath to the
	// certificate ends with a supported extension
	extension := filepath.Ext(cert.RealPath)
	if len(extension) == 0 || !strutil.ListContains(allowedSuffixes, extension[1:]) {
		return true
	}
	return strutil.ListContains(blockedCertDigests, cert.Sha256)
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
		if caFile.IsDir() {
			continue
		}

		extension := filepath.Ext(caFile.Name())
		if len(extension) == 0 || !strutil.ListContains(allowedSuffixes, extension[1:]) {
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

		// Load the cert file and calculate the digest of the certificate.
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
			Name:            trimExtension(caFile.Name()),
			Path:            filepath.Join(certsPath, caFile.Name()),
			RealPath:        certRealPath,
			Sha256:          cert.Sha256,
			SubjectNameSha1: cert.SubjectNameSha1,
		}
		certsObjects = append(certsObjects, certObject)
	}
	return certsObjects, nil
}

// readDigests reads the names of all files in the given directory
// and returns them as a list of strings (with any extension trimmed).
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
	// and we trim any extension prior to returning them.
	var digests []string
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		name := trimExtension(f.Name())
		digests = append(digests, name)
	}
	return digests, nil
}

// writeUniqueCACertificates writes the merged CA bundle and populates the
// merged directory with one link per distinct certificate payload. For
// single-certificate files it also creates the OpenSSL-style subject hash link.
func writeUniqueCACertificates(certs *certificates, certsDir string, bundle io.Writer) error {
	copyOne := func(from string) error {
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}

		// Append it to the ca bundle
		if _, err := io.Writer.Write(bundle, data); err != nil {
			return err
		}

		// Create a copy of it into the merged directory, to preserve the
		// structure of the system certificates.
		to := filepath.Join(certsDir, filepath.Base(from))
		return os.WriteFile(to, data, 0o644)
	}

	// Create the c_rehash-style subject hash link for single-certificate files.
	sha1Link := func(cert certificate) error {
		if cert.SubjectNameSha1 == "" {
			return nil
		}

		// Emulate https://docs.openssl.org/1.0.2/man1/c_rehash/ behaviour
		// for creating a hash lookup. It must be in SHA-1.
		hash := cert.SubjectNameSha1
		if len(hash) > 8 {
			hash = hash[:8]
		}

		for suffix := 0; ; suffix++ {
			linkName := filepath.Join(certsDir, fmt.Sprintf("%s.%d", hash, suffix))
			from := filepath.Join(certsDir, filepath.Base(cert.RealPath))
			if err := os.Link(from, linkName); err != nil {
				if os.IsExist(err) {
					continue
				}
				return err
			}
			return nil
		}
	}

	// avoid adding digests twice
	digests := make(map[string]bool)

	for _, cert := range certs.SystemCertificates {
		if digests[cert.Sha256] || isBlocked(cert, certs.BlockedDigests) {
			continue
		}
		if err := copyOne(cert.RealPath); err != nil {
			return fmt.Errorf("cannot copy certificate %q: %v", cert.Name, err)
		}
		if err := sha1Link(cert); err != nil {
			return fmt.Errorf("cannot create hash link for certificate %q: %v", cert.Name, err)
		}
		digests[cert.Sha256] = true
	}

	for _, cert := range certs.AddedCertificates {
		if digests[cert.Sha256] || isBlocked(cert, certs.BlockedDigests) {
			continue
		}
		if err := copyOne(cert.RealPath); err != nil {
			return fmt.Errorf("cannot copy extra certificate %q: %v", cert.Name, err)
		}
		if err := sha1Link(cert); err != nil {
			return fmt.Errorf("cannot create hash link for extra certificate %q: %v", cert.Name, err)
		}
		digests[cert.Sha256] = true
	}

	// sync the directory to ensure file-writes are completed
	dir, err := os.Open(certsDir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// generateCACertificates builds a merged certificate directory that mirrors
// the system /etc/ssl/certs layout: individual certificate links plus a
// combined ca-certificates.crt bundle. The directory is assembled in a
// temporary location and atomically renamed into place so a failure mid-build
// never leaves the final path in an inconsistent state.
func generateCACertificates(certs *certificates, mergedPath string) error {
	tmpMergedPath := mergedPath + ".tmp"

	// Remove any existing temp directory from a previous failed attempt,
	// and recreate the directory.
	os.RemoveAll(tmpMergedPath)
	if err := os.MkdirAll(tmpMergedPath, 0o755); err != nil {
		return fmt.Errorf("cannot create merged certificates directory: %v", err)
	}

	// Clean up the temp dir on failure so we don't leave partial state.
	defer func() {
		if e2 := os.RemoveAll(tmpMergedPath); e2 != nil {
			logger.Noticef("Failed to remove old certificates directory %q: %v", tmpMergedPath, e2)
		}
	}()

	bundlePath := filepath.Join(tmpMergedPath, "ca-certificates.crt")
	bundle, err := os.Create(bundlePath)
	if err != nil {
		return fmt.Errorf("cannot create ca-certificates.crt: %v", err)
	}
	defer bundle.Close()

	// Fill the bundle and create cert links, all inside the temp dir.
	if err := writeUniqueCACertificates(certs, tmpMergedPath, bundle); err != nil {
		return err
	}

	// Ensure the target directory exists so the swap has something to
	// exchange with. This is a no-op when regenerating an existing DB.
	if err := os.MkdirAll(mergedPath, 0o755); err != nil {
		return fmt.Errorf("cannot create merged certificates directory: %v", err)
	}

	if err := osutil.SwapDirs(tmpMergedPath, mergedPath); err != nil {
		return fmt.Errorf("cannot replace certificates directory: %v", err)
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
// /var/lib/snapd/pki/v1/<name>.crt
func ensureDirectories() error {
	dirsToEnsure := []string{
		filepath.Join(dirs.SnapdPKIV1Dir, "added"),
		filepath.Join(dirs.SnapdPKIV1Dir, "blocked"),
	}
	for _, dir := range dirsToEnsure {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot create directory %q: %v", dir, err)
		}
	}
	return nil
}

var GenerateCertificateDatabase = GenerateCertificateDatabaseImpl

// GenerateCertificateDatabaseImpl generates a merged certificate directory at
// /var/lib/snapd/pki/v1/merged/ that mirrors the system /etc/ssl/certs layout.
// It combines:
//   - /etc/ssl/certs/ (base certificates from the system)
//   - /var/lib/snapd/pki/v1/added/ (user added certificates)
//   - /var/lib/snapd/pki/v1/blocked/ (user blocked certificate digests)
//
// The resulting directory contains individual certificate links plus a combined
// ca-certificates.crt bundle. The directory is built in a temporary location
// and atomically swapped into place.
func GenerateCertificateDatabaseImpl() error {
	if err := ensureDirectories(); err != nil {
		return err
	}

	certs, err := loadCertificates()
	if err != nil {
		return err
	}

	mergedDir := filepath.Join(dirs.SnapdPKIV1Dir, "merged")
	return generateCACertificates(certs, mergedDir)
}

// certificatePathWithExtension returns a path under dir for a certificate name
// stored with the on-disk .crt suffix.
func certificatePathWithExtension(dir, name string) string {
	return filepath.Join(dir, name+".crt")
}

// CertificatePath returns a path to the certificate file itself,
// given the name of the certificate (without .crt extension).
// Custom certificates are expected to be with .crt extension, while
// system certificates may vary.
func CertificatePath(name string) string {
	return certificatePathWithExtension(dirs.SnapdPKIV1Dir, name)
}

// RemoveCertificateSymlinks removes the symlinks for the given certificate digest
// from the added and blocked directories.
func RemoveCertificateSymlinks(digest string) error {
	addedDir := filepath.Join(dirs.SnapdPKIV1Dir, "added")
	blockedDir := filepath.Join(dirs.SnapdPKIV1Dir, "blocked")

	if err := os.Remove(certificatePathWithExtension(addedDir, digest)); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(certificatePathWithExtension(blockedDir, digest)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// RemoveCertificate removes the certificate file for the given name. This does
// not remove the symlinks in the added/blocked directories.
func RemoveCertificate(name string) error {
	if err := os.Remove(certificatePathWithExtension(dirs.SnapdPKIV1Dir, name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// WriteCertificate writes the given contents as a new certificate file. Does not
// set the state of the certificate (i.e. does not create symlinks in the added/blocked directories).
func WriteCertificate(name, content string) error {
	certPath := certificatePathWithExtension(dirs.SnapdPKIV1Dir, name)
	if err := os.WriteFile(certPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("cannot write custom certificate %q: %v", name, err)
	}
	return nil
}

// SetCertificateState records the requested state for a custom certificate by
// creating the corresponding symlink in added or blocked. Callers that need to
// clear or replace an existing state must remove old symlinks separately.
func SetCertificateState(name, digest, state string) error {
	customPath := certificatePathWithExtension("..", name)

	switch state {
	case CertificateStateAccepted:
		addedDir := filepath.Join(dirs.SnapdPKIV1Dir, "added")
		addedPath := certificatePathWithExtension(addedDir, digest)
		if err := os.Symlink(customPath, addedPath); err != nil {
			return err
		}
	case CertificateStateBlocked:
		blockedDir := filepath.Join(dirs.SnapdPKIV1Dir, "blocked")
		blockedPath := certificatePathWithExtension(blockedDir, digest)
		if err := os.Symlink(customPath, blockedPath); err != nil {
			return err
		}
	}
	return nil
}

// CertificateInfo describes a custom certificate together with its configured
// state and original file contents.
type CertificateInfo struct {
	Name        string `json:"name"`
	Fingerprint string `json:"fingerprint"`
	State       string `json:"state"`
	Content     string `json:"content,omitempty"`
}

// certificateDigestAndContent reads a custom certificate file and returns its
// content fingerprint plus the original file contents.
func certificateDigestAndContent(name, baseDir string) (digest string, content string, err error) {
	certPath := certificatePathWithExtension(baseDir, name)
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return "", "", fmt.Errorf("cannot read certificate %q: %w", name, err)
	}

	cdata, err := ParseCertificateData(certBytes)
	if err != nil {
		return "", "", fmt.Errorf("cannot parse certificate %q: %w", name, err)
	}
	return cdata.Sha256, string(certBytes), nil
}

// certificateInfo resolves the fingerprint, content, and current state for a
// certificate stored under baseDir.
func certificateInfo(name, baseDir, addedDir, blockedDir string) (*CertificateInfo, error) {
	digest, content, err := certificateDigestAndContent(name, baseDir)
	if err != nil {
		return nil, err
	}

	state := CertificateStateUnset
	if osutil.IsSymlink(certificatePathWithExtension(blockedDir, digest)) {
		state = CertificateStateBlocked
	} else if osutil.IsSymlink(certificatePathWithExtension(addedDir, digest)) {
		state = CertificateStateAccepted
	}

	return &CertificateInfo{
		Name:        name,
		Fingerprint: digest,
		State:       state,
		Content:     content,
	}, nil
}

// CustomCertificateInfo returns the information about a custom certificate with
// the given name, including its fingerprint, state and content.
func CustomCertificateInfo(name string) (*CertificateInfo, error) {
	baseDir := dirs.SnapdPKIV1Dir
	addedDir := filepath.Join(baseDir, "added")
	blockedDir := filepath.Join(baseDir, "blocked")
	return certificateInfo(name, baseDir, addedDir, blockedDir)
}

// CustomCertificates returns the list of custom certificates with their name, fingerprint and state.
func CustomCertificates() ([]*CertificateInfo, error) {
	addedDir := filepath.Join(dirs.SnapdPKIV1Dir, "added")
	blockedDir := filepath.Join(dirs.SnapdPKIV1Dir, "blocked")

	// Read the contents of the custom certificates directory to get the list of all custom certificates, and their content and state.
	files, err := os.ReadDir(dirs.SnapdPKIV1Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var certsInfo []*CertificateInfo
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".crt") {
			continue
		}
		name := trimExtension(f.Name())
		info, err := certificateInfo(name, dirs.SnapdPKIV1Dir, addedDir, blockedDir)
		if err != nil {
			// Let us be resilient to errors here, and just skip the certificate if we
			// cannot read it or parse it, as we don't want one broken certificate to
			// cause the whole API to be unavailable.
			logger.Noticef("Failed to read custom certificate %q: %v", name, err)
			continue
		}
		if info != nil {
			certsInfo = append(certsInfo, info)
		}
	}
	return certsInfo, nil
}
