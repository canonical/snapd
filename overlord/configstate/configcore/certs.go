// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers
// +build !nomanagers

/*
 * Copyright (C) 2020 Canonical Ltd
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

package configcore

import (
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/strutil"
)

type certificate struct {
	Name     string
	Path     string
	RealPath string
}

// isBlocked This function validates the name of the certificate. We only allow certificates
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

// getCertObjects Helper function to retrieve a list of files in the directory path and returns
// them as objects with their name and real path (any symlinks will be evaluated). Each file object
// contains both the path of file, and the evaluated real path, which are identical if the file is
// not a symlink.
// TODO Should we support recursive directories inside the certs dir?
func getCertObjects(certsPath string) ([]certificate, error) {
	certFiles, err := ioutil.ReadDir(certsPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read certs directory: %v", err)
	}

	var certsObjects []certificate
	for _, cert := range certFiles {
		if cert.IsDir() {
			continue
		}

		// So what are we trying to achieve here? When provided with certificate directories
		// they may or may not be symbolic links to the actual certificate file. To handle this
		// correctly we evaluate the symbolic links and get the real path of the certificate, because
		// we are not going to 'copy' or link to the symbolic link, instead we will recreate the symbolic
		// link to the real path of the certificate.
		certRealPath := filepath.Join(certsPath, cert.Name())
		if cert.Mode()&os.ModeSymlink != 0 {
			resolvedPath, err := filepath.EvalSymlinks(certRealPath)
			if err != nil {
				return nil, fmt.Errorf("cannot resolve symbolic link: %v", err)
			}
			certRealPath = resolvedPath
		}

		// If the file is not a symbolic link then Path and RealPath will be identical.
		certObject := certificate{
			Name:     cert.Name(),
			Path:     filepath.Join(certsPath, cert.Name()),
			RealPath: certRealPath,
		}
		certsObjects = append(certsObjects, certObject)
	}
	return certsObjects, nil
}

// filterCerts Filters out the certificates that are not allowed to be merged.
func filterCerts(certs []certificate, blockedCerts []string) ([]certificate, error) {
	var filteredCerts []certificate
	for _, cert := range certs {
		if isBlocked(cert, blockedCerts) {
			continue
		}
		filteredCerts = append(filteredCerts, cert)
	}
	return filteredCerts, nil
}

func filterCertsInDirectory(dirPath string, blockedCerts []string) error {
	existingCerts, err := getCertObjects(dirPath)
	if err != nil {
		return err
	}

	// Remove the certs that are marked blocked.
	for _, cert := range existingCerts {
		if isBlocked(cert, blockedCerts) {
			err := os.Remove(cert.Path)
			if err != nil {
				return fmt.Errorf("cannot remove blocked certificate: %v", err)
			}
		}
	}
	return nil
}

// installCerts Populates symbolic links in the output directory, to each certificate
// provided. There may need to be some provide apparmor rules for the source directories
// and not just the merged (output) directory.
// TODO apparmor rules for the source directories?
func installCerts(outputPath string, certs []certificate) error {
	for _, cert := range certs {
		// When updating existing certifications we want to make sure we don't
		// overwrite the certification with itself.
		certDestinationPath := filepath.Join(outputPath, cert.Name)
		if certDestinationPath == cert.RealPath {
			continue
		}

		// If the path exists, then remove it first to update the certificate
		if _, err := os.Lstat(certDestinationPath); err == nil {
			err := os.Remove(certDestinationPath)
			if err != nil {
				return fmt.Errorf("cannot remove existing certificate: %v", err)
			}
		}

		// Create a link to the real path of the cert in the output path
		err := os.Symlink(cert.RealPath, certDestinationPath)
		if err != nil {
			return fmt.Errorf("cannot symlink store certificate: %v", err)
		}
	}

	return nil
}

// generateCACertificates Generate the ca-certificates.crt to the output path
// The ca-certificates.crt is a concatenation of all the certs in the
// output path.
func generateCACertificates(outputPath string) error {
	certificates, err := getCertObjects(outputPath)
	if err != nil {
		return err
	}

	certsPath := filepath.Join(outputPath, "ca-certificates.crt")
	certsFile, err := os.Create(certsPath)
	if err != nil {
		return fmt.Errorf("cannot create ca-certificates.crt: %v", err)
	}

	for _, cert := range certificates {
		certBytes, err := ioutil.ReadFile(cert.RealPath)
		if err != nil {
			return fmt.Errorf("cannot read certificate %q: %v", cert.Name, err)
		}

		_, err = certsFile.Write(certBytes)
		if err != nil {
			return fmt.Errorf("cannot write certificate %q: %v", cert.Name, err)
		}
	}

	return nil
}

func getCertObjectsFromPaths(outputPath string, inputPaths []string) ([]certificate, error) {
	var certObjects []certificate
	for _, certDirectoryPath := range inputPaths {
		// If the input directory equals output, then ignore it
		certFullPath, err := filepath.Abs(certDirectoryPath)
		if err != nil {
			return nil, fmt.Errorf("cannot get absolute path for path: %v", certDirectoryPath)
		}
		if certFullPath == outputPath {
			continue
		}

		certs, err := getCertObjects(certDirectoryPath)
		if err != nil {
			return nil, fmt.Errorf("cannot read certs: %v", err)
		}
		certObjects = append(certObjects, certs...)
	}
	return certObjects, nil
}

// CombineCertConfigurations Allows the caller to combine multiple certification directories into one single directory.
// Creates new symlinks in the output directory, following symlinks from
// the source directories, and generates the ca-certificates.crt file in the output folder.
func CombineCertConfigurations(outputPath string, certDirectoryPaths []string, blockedCerts []string) error {
	outputFullPath, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("cannot get absolute path for output path: %v", outputPath)
	}

	certObjects, err := getCertObjectsFromPaths(outputFullPath, certDirectoryPaths)
	if err != nil {
		return err
	}

	filteredCerts, err := filterCerts(certObjects, blockedCerts)
	if err != nil {
		return fmt.Errorf("cannot filter certs: %v", err)
	}

	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return fmt.Errorf("cannot create store ssl certs dir: %v", err)
	}

	// Filter out the certs that are not allowed to be merged, in the output
	// directory, as we may or may not have certs already in destination path.
	if err := filterCertsInDirectory(outputPath, blockedCerts); err != nil {
		return fmt.Errorf("cannot filter output directory: %v", err)
	}

	if err := installCerts(outputPath, filteredCerts); err != nil {
		return fmt.Errorf("cannot copy certs to output: %v", err)
	}

	if err := generateCACertificates(outputPath); err != nil {
		return fmt.Errorf("cannot generate ca-certificates.crt: %v", err)
	}
	return nil
}

func handleCertConfiguration(tr config.Conf, opts *fsOnlyContext) error {
	// This handles the "snap revert core" case:
	// We need to go over each pem cert on disk and check if there is
	// a matching config entry - if not->delete the cert
	//
	// XXX: remove this code once we have a general way to handle
	//      "snap revert" and config updates
	//

	// TODO: add ways to detect cleanly if tr is a patch, skip the sync code if it is
	storeCerts, err := filepath.Glob(filepath.Join(dirs.SnapdStoreSSLCertsDir, "*.pem"))
	if err != nil {
		return fmt.Errorf("cannot get exiting store certs: %v", err)
	}
	for _, storeCertPath := range storeCerts {
		optionName := strings.TrimSuffix(filepath.Base(storeCertPath), ".pem")
		v, err := coreCfg(tr, "store-certs."+optionName)
		if err != nil {
			return err
		}
		if v == "" {
			if err := os.Remove(storeCertPath); err != nil {
				return err
			}
		}
	}

	// add/remove regular (non revert) changes
	for _, name := range tr.Changes() {
		if !strings.HasPrefix(name, "core.store-certs.") {
			continue
		}

		nameWithoutSnap := strings.SplitN(name, ".", 2)[1]
		cert, err := coreCfg(tr, nameWithoutSnap)
		if err != nil {
			return fmt.Errorf("internal error: cannot get data for %s: %v", nameWithoutSnap, err)
		}
		optionName := strings.SplitN(name, ".", 3)[2]
		certPath := filepath.Join(dirs.SnapdStoreSSLCertsDir, optionName+".pem")
		switch cert {
		case "":
			// remove
			if err := os.Remove(certPath); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("cannot remove store certificate: %v", err)
			}
		default:
			if err := os.MkdirAll(dirs.SnapdStoreSSLCertsDir, 0755); err != nil {
				return fmt.Errorf("cannot create store ssl certs dir: %v", err)
			}
			if err := ioutil.WriteFile(certPath, []byte(cert), 0644); err != nil {
				return fmt.Errorf("cannot write store certificate: %v", err)
			}
		}
	}

	return nil
}

func validateCertSettings(tr config.Conf) error {
	for _, name := range tr.Changes() {
		if !strings.HasPrefix(name, "core.store-certs.") {
			continue
		}

		nameWithoutSnap := strings.SplitN(name, ".", 2)[1]
		cert, err := coreCfg(tr, nameWithoutSnap)
		if err != nil {
			return fmt.Errorf("internal error: cannot get data for %s: %v", nameWithoutSnap, err)
		}
		if cert != "" {
			optionName := strings.SplitN(name, ".", 3)[2]
			if !validCertName(optionName) {
				return fmt.Errorf("cannot set store ssl certificate under name %q: name must only contain word characters or a dash", optionName)
			}
			cp := x509.NewCertPool()
			if !cp.AppendCertsFromPEM([]byte(cert)) {
				return fmt.Errorf("cannot decode pem certificate %q", optionName)
			}
		}
	}

	return nil
}
