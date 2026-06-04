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
	"sort"
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

// CertificateData holds the parsed certificate and derived digests for a
// certificate payload.
type CertificateData struct {
	// Sha256 is the semantic content fingerprint tracked for the certificate
	// payload. It is derived from parsed certificate blocks, so equivalent PEM
	// formatting changes do not change the digest.
	Sha256 string
	// SubjectNameSha1 is the OpenSSL subject-name hash used for lookup links.
	SubjectNameSha1 string
}

type certificate struct {
	Name            string
	Path            string
	RealPath        string
	Sha256          string
	SubjectNameSha1 string
}

// certificateManifest captures the published-tree identity snapd cares
// about: semantic certificate content together with the visible filenames and
// hash-link targets it renders.
type certificateManifest struct {
	records []string
}

func (m *certificateManifest) addFile(name, digest string) {
	m.records = append(m.records, fmt.Sprintf("file\x00%s\x00%s", name, digest))
}

func (m *certificateManifest) addLink(name, target string) {
	m.records = append(m.records, fmt.Sprintf("symlink\x00%s\x00%s", name, target))
}

// hash returns the published-generation key for the rendered tree.
// The key intentionally follows functional certificate state rather than the
// exact bytes produced while copying PEM files into the bundle.
func (m *certificateManifest) hash() string {
	h := sha256.New224()
	_, _ = io.WriteString(h, "rendered-certificates-manifest-v1\x00")

	// Sort records so generation names stay stable even if future rendering
	// paths enqueue equivalent files and links in a different order.
	sortedRecords := append([]string(nil), m.records...)
	sort.Strings(sortedRecords)
	for _, record := range sortedRecords {
		_, _ = io.WriteString(h, record)
		_, _ = io.WriteString(h, "\x00")
	}

	return fmt.Sprintf("%x", h.Sum(nil))
}

// TODO: .crl not supported for now, and there is none of this type carried
// in the bases
var allowedSuffixes = []string{"pem", "crt", "cer"}

// certificatePEMBlockTypePattern matches the PEM labels accepted when scanning
// certificate files or bundles.
var certificatePEMBlockTypePattern = regexp.MustCompile(`^(X509 |TRUSTED |)?CERTIFICATE$`)

// sha256HexForChain returns the semantic content fingerprint for a certificate
// payload. For PEM bundles, every certificate DER block contributes to the
// digest in file order so two files that share a leaf certificate but differ
// elsewhere do not collapse to the same value. PEM comments and line wrapping
// are intentionally ignored.
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
// a runtime error in all cases. We have other places in Snapd that are affected by this
// too, so this should be dealt with together.
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
// We only return the 'raw' certificate data for the first PEM block, which is used
// for the subject name hash, and only used when there are not multiple certificates in the file.
func decodePemBlocks(data []byte) (blocks [][]byte, raw *x509.Certificate, err error) {
	rest := data
	for {
		block, next := pem.Decode(rest)
		if block == nil {
			break
		}
		rest = next
		if !certificatePEMBlockTypePattern.MatchString(block.Type) {
			logger.Debugf("encountered unsupported pem-block type: %s", block.Type)
			continue
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("cannot parse certificate PEM block: %v", err)
		}
		if raw == nil {
			raw = cert
		}
		blocks = append(blocks, cert.Raw)
	}
	return blocks, raw, nil
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
		// We only use the 'raw' certificate data when one PEM block is present
		blocks, raw, err := decodePemBlocks(certData)
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
			hash, err := sha1HexForCertSubjectName(raw)
			if err != nil {
				return nil, fmt.Errorf("cannot hash certificate subject name: %v", err)
			}
			subjectNameSha1 = hash
		}

		return &CertificateData{
			Sha256:          sha256HexForChain(blocks),
			SubjectNameSha1: subjectNameSha1,
		}, nil
	}

	// The file is not PEM-encoded, so we try to parse it as DER.
	// We return a single-certificate chain in this case.
	cert, err := x509.ParseCertificate(certData)
	if err != nil {
		return nil, fmt.Errorf("cannot parse DER certificate: %v", err)
	}

	subjectNameSha1, err := sha1HexForCertSubjectName(cert)
	if err != nil {
		return nil, fmt.Errorf("cannot hash certificate subject name: %v", err)
	}
	return &CertificateData{
		Sha256:          sha256HexForChain([][]byte{cert.Raw}),
		SubjectNameSha1: subjectNameSha1,
	}, nil
}

func isAllowedExtension(name string) bool {
	ext := filepath.Ext(name)
	return len(ext) != 0 && strutil.ListContains(allowedSuffixes, ext[1:])
}

// trimExtension strips one supported certificate-file extension from name.
func trimExtension(name string) string {
	extension := filepath.Ext(name)
	if isAllowedExtension(name) {
		return strings.TrimSuffix(name, extension)
	}
	return name
}

// isBlocked checks whether the given certificate is blocked
// based on its name (special names or its path not being a supported extension), or
// based on its digest being in the list of blocked digests.
func isBlocked(cert certificate, blockedCertDigests []string) bool {
	// Special case for ca-certificates.crt
	if cert.Name == "ca-certificates" {
		return true
	}

	// Check that the real underlying filepath to the
	// certificate ends with a supported extension
	if !isAllowedExtension(cert.RealPath) {
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

		if !isAllowedExtension(caFile.Name()) {
			continue
		}

		// When provided with certificate directories they may be symbolic links to
		// the actual certificate file.
		certRealPath := filepath.Join(certsPath, caFile.Name())
		if caFile.Type()&os.ModeSymlink != 0 {
			resolvedPath, err := filepath.EvalSymlinks(certRealPath)
			if err != nil {
				logger.Noticef("cannot parse certificate %q: cannot resolve symbolic link: %v", certRealPath, err)
				continue
			}
			certRealPath = resolvedPath
		}

		// Load the cert file and calculate the digest of the certificate.
		certBytes, err := os.ReadFile(certRealPath)
		if err != nil {
			logger.Noticef("cannot read certificate %q: %v", certRealPath, err)
			continue
		}

		cert, err := ParseCertificateData(certBytes)
		if err != nil {
			logger.Noticef("cannot parse certificate %q: %v", certRealPath, err)
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
func writeUniqueCACertificates(certs *certificates, certsDir string, bundle io.Writer, manifest *certificateManifest) error {
	usedOutputNames := make(map[string]string)

	// Keep source basenames when possible so merged still resembles the system
	// certificates layout, but fall back to a digest-derived name when a
	// distinct certificate would otherwise overwrite an existing copy.
	outputNameFor := func(cert certificate) string {
		base := filepath.Base(cert.RealPath)
		if ownerDigest, ok := usedOutputNames[base]; !ok || ownerDigest == cert.Sha256 {
			usedOutputNames[base] = cert.Sha256
			return base
		}

		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		candidate := fmt.Sprintf("%s-%s%s", stem, cert.Sha256, ext)
		if ownerDigest, ok := usedOutputNames[candidate]; !ok || ownerDigest == cert.Sha256 {
			usedOutputNames[candidate] = cert.Sha256
			return candidate
		}

		for suffix := 1; ; suffix++ {
			candidate = fmt.Sprintf("%s-%s-%d%s", stem, cert.Sha256, suffix, ext)
			if ownerDigest, ok := usedOutputNames[candidate]; !ok || ownerDigest == cert.Sha256 {
				usedOutputNames[candidate] = cert.Sha256
				return candidate
			}
		}
	}

	copyOne := func(cert certificate, outputName string) error {
		data, err := os.ReadFile(cert.RealPath)
		if err != nil {
			return err
		}

		// Append it to the ca bundle
		if _, err := io.Writer.Write(bundle, data); err != nil {
			return err
		}

		// Create a copy in the merged directory under the chosen unique name.
		to := filepath.Join(certsDir, outputName)
		if err := os.WriteFile(to, data, 0o644); err != nil {
			return err
		}
		manifest.addFile(outputName, cert.Sha256)
		return nil
	}

	// Create the c_rehash-style subject hash link for single-certificate files.
	maybeSha1Link := func(cert certificate, outputName string) error {
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
			linkBase := fmt.Sprintf("%s.%d", hash, suffix)
			linkName := filepath.Join(certsDir, linkBase)
			// The merged directory may be built in a staging location and then
			// atomically swapped into place, so the hash link must stay relative
			// to the certificate copy that lives alongside it.
			if err := os.Symlink(outputName, linkName); err != nil {
				if os.IsExist(err) {
					continue
				}
				return err
			}
			manifest.addLink(linkBase, outputName)
			return nil
		}
	}

	// avoid adding digests twice
	digests := make(map[string]bool)

	for _, cert := range certs.SystemCertificates {
		if digests[cert.Sha256] || isBlocked(cert, certs.BlockedDigests) {
			continue
		}
		outputName := outputNameFor(cert)
		if err := copyOne(cert, outputName); err != nil {
			return fmt.Errorf("cannot copy certificate %q: %v", cert.Name, err)
		}
		if err := maybeSha1Link(cert, outputName); err != nil {
			return fmt.Errorf("cannot create hash link for certificate %q: %v", cert.Name, err)
		}
		digests[cert.Sha256] = true
	}

	for _, cert := range certs.AddedCertificates {
		if digests[cert.Sha256] || isBlocked(cert, certs.BlockedDigests) {
			continue
		}
		outputName := outputNameFor(cert)
		if err := copyOne(cert, outputName); err != nil {
			return fmt.Errorf("cannot copy extra certificate %q: %v", cert.Name, err)
		}
		if err := maybeSha1Link(cert, outputName); err != nil {
			return fmt.Errorf("cannot create hash link for extra certificate %q: %v", cert.Name, err)
		}
		digests[cert.Sha256] = true
	}
	return nil
}

// generateCACertificates builds a merged certificate directory that mirrors
// the system /etc/ssl/certs layout: individual certificate links plus a
// combined ca-certificates.crt bundle. It also returns the render metadata
// needed to name the published generation after the semantic directory view.
func generateCACertificates(certs *certificates, mergedPath string) (*certificateManifest, error) {
	if err := os.MkdirAll(mergedPath, 0o755); err != nil {
		return nil, fmt.Errorf("cannot create merged certificates directory: %v", err)
	}

	bundlePath := filepath.Join(mergedPath, "ca-certificates.crt")
	bundle, err := osutil.NewAtomicFile(bundlePath, 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return nil, fmt.Errorf("cannot create ca-certificates.crt: %v", err)
	}
	defer bundle.Cancel()

	manifest := &certificateManifest{}

	// Fill the bundle and create cert links, all inside the merged directory.
	if err := writeUniqueCACertificates(certs, mergedPath, bundle, manifest); err != nil {
		return nil, err
	}
	if err := bundle.Commit(); err != nil {
		return nil, fmt.Errorf("cannot commit ca-certificates.crt: %v", err)
	}
	// sync the directory to ensure file-writes are completed
	dir, err := os.Open(mergedPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open merged directory: %v", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return nil, fmt.Errorf("cannot sync merged directory: %v", err)
	}
	return manifest, nil
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
	if exists, isDir, err := osutil.DirExists(filepath.Join(dirs.SnapdPKIV1Dir, "added")); err != nil {
		// Fail closed here: if we cannot inspect the added-certs directory we
		// cannot safely compute the trust view.
		return nil, err
	} else if exists && isDir {
		addedDir := filepath.Join(dirs.SnapdPKIV1Dir, "added")
		a, err := parseCertificates(addedDir)
		if err != nil {
			return nil, err
		}

		certs.AddedCertificates = a
	}

	// If the blocked directory exists, read the digests
	if exists, isDir, err := osutil.DirExists(filepath.Join(dirs.SnapdPKIV1Dir, "blocked")); err != nil {
		// Fail closed here too: ignoring blocked-digest lookup failures could
		// accidentally re-enable certificates the administrator meant to suppress.
		return nil, err
	} else if exists && isDir {
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
// /var/lib/snapd/pki/v1/published/<generation>/*.crt
// /var/lib/snapd/pki/v1/published/<generation>/ca-certificates.crt
// /var/lib/snapd/pki/v1/merged -> published/<generation>
// /var/lib/snapd/pki/v1/<name>.crt
func ensureDirectories() error {
	dirsToEnsure := []string{
		filepath.Join(dirs.SnapdPKIV1Dir, "added"),
		filepath.Join(dirs.SnapdPKIV1Dir, "blocked"),
		filepath.Join(dirs.SnapdPKIV1Dir, "published"),
	}
	for _, dir := range dirsToEnsure {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot create directory %q: %v", dir, err)
		}
	}
	return nil
}

// Certificate publication uses the following structure:
//   - published/<generation> holds immutable rendered certificate trees
//   - merged points at the active generation consumers should resolve
//
// The helpers below maintain that contract by publishing via pointer swaps
// instead of mutating an existing generation in place. Undo keeps its own
// rollback target in persisted task state rather than in a separate symlink.

// CurrentCertificateDir returns the compatibility path that consumers follow
// for the active certificate view while snapd publishes immutable generations
// alongside it.
func CurrentCertificateDir() string {
	return filepath.Join(dirs.SnapdPKIV1Dir, "merged")
}

// PublishedCertificatesDir holds immutable certificate generations so updates
// can move the active view forward without rewriting trees that may still be in
// use elsewhere.
func PublishedCertificatesDir() string {
	return filepath.Join(dirs.SnapdPKIV1Dir, "published")
}

// mergedCertificatesGeneration returns the relative target used by the public
// generation pointers so they keep working if the snapd state directory moves
// under a different root.
func mergedCertificatesGeneration(generation string) string {
	return filepath.Join("published", generation)
}

// switchCertificatesLink atomically replaces one of the generation pointers so
// readers never have to observe a half-updated or missing link.
func switchCertificatesLink(linkPath, target string) error {
	tmpLink := linkPath + ".new"
	if err := os.Remove(tmpLink); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Symlink(target, tmpLink); err != nil {
		return err
	}
	if err := os.Rename(tmpLink, linkPath); err != nil {
		_ = os.Remove(tmpLink)
		return err
	}
	return nil
}

// switchCurrentMergedCertificates updates the current "merged" pointer that
// consumers resolve, so publishing a new generation stays a metadata change.
func switchCurrentMergedCertificates(target string) error {
	return switchCertificatesLink(CurrentCertificateDir(), target)
}

// resolveCurrentCertificateTarget resolves the active published generation target,
// and returns an empty string if the link is missing (e.g. first run or after cleanup).
func resolveCurrentCertificateTarget() (string, error) {
	mergedDir := CurrentCertificateDir()
	info, err := os.Lstat(mergedDir)
	if err != nil {
		// no merged directory?
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	if info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Errorf("merged certificates path is not a symlink")
	}

	target, err := os.Readlink(mergedDir)
	if err != nil {
		return "", err
	}
	return target, nil
}

// certificateGenerations returns the immutable published generation names.
// Garbage collection only reasons about these directories; the public symlinks
// and other metadata are handled separately.
func certificateGenerations() ([]string, error) {
	entries, err := os.ReadDir(PublishedCertificatesDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("cannot read published certificates directory: %v", err)
	}

	var generations []string
	for _, entry := range entries {
		if entry.IsDir() {
			generations = append(generations, entry.Name())
		}
	}
	return generations, nil
}

// garbageCollectCertificateGenerations uses a two-boot cleanup policy for
// non-current generations. The first pass only marks an inactive generation;
// a later boot removes it if nothing has made it current again in the
// meantime. This keeps cleanup away from the publication path and gives other
// parts of the system time to stop referencing an older tree.
func garbageCollectCertificateGenerations(bootID string) error {
	currentTarget, err := resolveCurrentCertificateTarget()
	if err != nil {
		return err
	}

	generations, err := certificateGenerations()
	if err != nil {
		return err
	}

	for _, generation := range generations {
		target := mergedCertificatesGeneration(generation)
		genPath := filepath.Join(PublishedCertificatesDir(), generation)
		inactiveFile := filepath.Join(genPath, ".snapd-inactive")

		if target == currentTarget {
			// If a generation became current again, clear any stale inactivity mark
			// so the next boot does not treat the live tree as pending deletion.
			if osutil.FileExists(inactiveFile) {
				if err := os.Remove(inactiveFile); err != nil {
					return fmt.Errorf("cannot remove %q: %v", inactiveFile, err)
				}
			}
			continue
		}

		if osutil.FileExists(inactiveFile) {
			data, err := os.ReadFile(inactiveFile)
			if err != nil {
				return fmt.Errorf("cannot read %q: %v", inactiveFile, err)
			}
			if string(data) != bootID {
				logger.Debugf("garbage collecting certificate generation %s", generation)
				if err := os.RemoveAll(genPath); err != nil {
					return fmt.Errorf("cannot remove old generation at %q: %v", genPath, err)
				}
			}
		} else {
			// Mark the generation first and only delete it on a later boot so GC
			// does not race the publication step or long-lived readers of the old tree.
			if err := os.WriteFile(inactiveFile, []byte(bootID), 0o644); err != nil {
				return fmt.Errorf("cannot write %q: %v", inactiveFile, err)
			}
		}
	}
	return nil
}

// RefreshCertificateDatabase does a best-effort of performing an
// atomic update of the existing cert database. Expects state to be
// locked when calling this function, to avoid concurrent updates to the database.
var RefreshCertificateDatabase = refreshCertificateDatabaseImpl

func refreshCertificateDatabaseImpl() error {
	// Re-entry is safe here because publication is one-way and content-aware:
	// we build the next tree in a temporary directory, publish it under its
	// deterministic generation name, and only then flip merged.
	if err := ensureDirectories(); err != nil {
		return err
	}

	publishedDir := PublishedCertificatesDir()
	currentTarget, err := resolveCurrentCertificateTarget()
	if err != nil {
		return err
	}

	// Build the next certificate view off to the side so the active generation
	// stays unchanged until publication is reduced to metadata updates.
	stagedDir, err := os.MkdirTemp(publishedDir, ".generation-")
	if err != nil {
		return fmt.Errorf("cannot create staging directory for published certificates: %v", err)
	}
	defer os.RemoveAll(stagedDir)

	certs, err := loadCertificates()
	if err != nil {
		return err
	}

	manifest, err := generateCACertificates(certs, stagedDir)
	if err != nil {
		return err
	}

	// Name published generations after semantic certificate content plus the
	// visible filenames and hash-link targets. Formatting-only PEM changes reuse
	// the existing immutable generation instead of republishing equivalent bytes.
	hash := manifest.hash()
	nextTarget := mergedCertificatesGeneration(hash)
	nextPath := filepath.Join(dirs.SnapdPKIV1Dir, nextTarget)
	if exists, isDir, err := osutil.DirExists(nextPath); err != nil {
		return err
	} else if !exists {
		if err := os.Rename(stagedDir, nextPath); err != nil {
			return fmt.Errorf("cannot publish certificates generation %q: %v", hash, err)
		}
	} else if !isDir {
		return fmt.Errorf("published certificates generation %q is not a directory", hash)
	}

	// If the current pointer already resolves to this generation, there is no
	// visible state change to publish.
	if currentTarget == nextTarget {
		return nil
	}

	// Publish by moving the public pointer, not by mutating generation
	// contents. A failure before the final pointer swap leaves the active
	// generation unchanged; a failure after publishing the immutable
	// generation but before switching merged lets a later retry reuse that
	// published tree and retry only the metadata update. Callers that need
	// rollback across reboot must still persist the previous
	// merged target themselves before calling into this helper.
	return switchCurrentMergedCertificates(nextTarget)
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
	if err := os.WriteFile(certPath, []byte(content), 0644); err != nil {
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
		if err := os.Symlink(customPath, addedPath); err != nil && !os.IsExist(err) {
			return err
		}
	case CertificateStateBlocked:
		blockedDir := filepath.Join(dirs.SnapdPKIV1Dir, "blocked")
		blockedPath := certificatePathWithExtension(blockedDir, digest)
		if err := os.Symlink(customPath, blockedPath); err != nil && !os.IsExist(err) {
			return err
		}
	}
	// We don't handle Unset currently because for the case of unsetting it actually
	// means removing the certificate currently (in the config api), which means RemoveCertificateSymlinks
	// is being called instead for the case of Unset.
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
			logger.Noticef("cannot read custom certificate %q: %v", name, err)
			continue
		}
		if info != nil {
			certsInfo = append(certsInfo, info)
		}
	}
	return certsInfo, nil
}
