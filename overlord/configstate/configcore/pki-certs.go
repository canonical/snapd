// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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

package configcore

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/snapcore/snapd/overlord/certstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
)

const (
	customCertPrefix = "pki.certs.custom"
)

func init() {
	supportedConfigurations["core."+customCertPrefix] = true
	config.RegisterExternalConfig("core", customCertPrefix, handleGetCustomCertificates)
}

// parseCustomCertKey parses a config key in the format of
// "pki.certs.custom.<name>[.<field>]"
func parseCustomCertKey(key string) (name, field string, err error) {
	if key == customCertPrefix {
		return "", "", nil
	}

	parts := strings.Split(key, ".")
	if len(parts) < 4 {
		return "", "", fmt.Errorf("cannot parse custom certificate option %q", key)
	}

	// the certificate name is the part after "pki.certs.custom." and before the optional field
	name = parts[3]
	if !validCertName(name) {
		return "", "", fmt.Errorf("invalid certificate name for %q: %q", key, name)
	}

	if len(parts) == 4 {
		return parts[3], "", nil
	} else if len(parts) == 5 {
		return parts[3], parts[4], nil
	}
	return "", "", fmt.Errorf("cannot parse custom certificate option %q", key)
}

type certificate struct {
	Name       string `json:"name"`
	Content    string `json:"content"`
	State      string `json:"state"`
	HasContent bool
}

func maybeExistingCertificate(name string, certs map[string]certificate) (certificate, error) {
	var cert certificate
	if existing, ok := certs[name]; ok {
		cert = existing
	} else {
		// Load existing certificate data for the certificate if it exists,
		// so that we can apply changes on top of it.
		cert = certificate{
			Name:  name,
			State: certstate.CertificateStateAccepted,
		}
		info, err := certstate.CustomCertificateInfo(name)
		if err == nil {
			cert.State = info.State
			cert.Content = info.Content
			cert.HasContent = true
		} else if !errors.Is(err, os.ErrNotExist) {
			return cert, fmt.Errorf("cannot load existing certificate data for %q: %v", name, err)
		}
	}
	return cert, nil
}

func gatherCertificates(tr RunTransaction) (map[string]certificate, error) {
	certs := make(map[string]certificate)
	changes := tr.Changes()
	for _, change := range changes {
		if !strings.HasPrefix(change, "core."+customCertPrefix) {
			continue
		}

		key := strings.TrimPrefix(change, "core.")
		name, field, err := parseCustomCertKey(key)
		if err != nil {
			return nil, err
		}

		// Name of a certificate must be provided
		if name == "" {
			continue
		}

		// If field is empty, assume that we are unsetting the certificate
		if field == "" {
			var v any
			if err := tr.Get("core", key, &v); err != nil && !config.IsNoOption(err) {
				return nil, err
			}
			if v != nil {
				return nil, fmt.Errorf("cannot set %q directly", key)
			}

			// Unsetting a certificate object removes the certificate.
			certs[name] = certificate{
				Name:       name,
				Content:    "",
				State:      certstate.CertificateStateUnset,
				HasContent: true,
			}
			continue
		}

		// ensure the certificate is in the map so that we can apply all changes to it together later
		cert, err := maybeExistingCertificate(name, certs)
		if err != nil {
			return nil, err
		}

		// retrieve the new value for the changed field and update the certificate in the map
		v, err := coreCfg(tr, key)
		if err != nil {
			return nil, err
		}

		switch field {
		case "content":
			cert.Content = v
			cert.HasContent = true
			if v == "" {
				// Indicate we are removing the certificate
				cert.State = certstate.CertificateStateUnset
			}
		case "state":
			cert.State = v
		case "name":
			// remove the old name from the map so that it doesn't get processed
			// later as a separate certificate, and update the certificate with the new name.
			// The actual rename operation will be handled later.

			// Make a copy with the new name
			certs[v] = certificate{
				Name:       v,
				Content:    cert.Content,
				State:      cert.State,
				HasContent: cert.HasContent,
			}

			// Clear content and state from the old cert to avoid
			// processing it as a separate certificate later
			cert.Content = ""
			cert.HasContent = true
			cert.State = certstate.CertificateStateUnset
		default:
			return nil, fmt.Errorf("unexpected field %q in custom certificate change", field)
		}
		certs[name] = cert
	}
	return certs, nil
}

func certificateFingerprint(certContent string) (string, error) {
	cdata, err := certstate.ParseCertificateData([]byte(certContent))
	if err != nil {
		return "", err
	}
	return cdata.Digest, nil
}

func ensureCertificateState(fp string, cert certificate) error {
	if err := certstate.RemoveCertificateSymlinks(fp); err != nil {
		return fmt.Errorf("cannot remove existing symlinks for custom certificate %q: %v", cert.Name, err)
	}

	if err := certstate.SetCertificateState(cert.Name, fp, cert.State); err != nil {
		return fmt.Errorf("cannot set state for custom certificate %q: %v", cert.Name, err)
	}
	return nil
}

func handleCustomCertificateRequest(tr RunTransaction, opts *fsOnlyContext) error {
	certs, err := gatherCertificates(tr)
	if err != nil {
		return err
	}
	if len(certs) == 0 {
		return nil
	}

	// Refresh certificates in the pki directory
	for _, cert := range certs {
		fpKey := fmt.Sprintf("%s.%s.fingerprint", customCertPrefix, cert.Name)

		// Get the old fingerprint to know if we need to remove the old certificate file and symlinks.
		// If the fingerprint is empty, it means this is a new certificate,
		// so we don't need to worry about removing old files.
		fpOld, err := coreCfg(tr, fpKey)
		if err != nil {
			return err
		}

		switch cert.State {
		case certstate.CertificateStateUnset:
			// Unsetting the certificate, we want to remove it.
			if fpOld != "" {
				if err := certstate.RemoveCertificateSymlinks(fpOld); err != nil {
					return fmt.Errorf("cannot remove existing symlinks for custom certificate %q: %v", cert.Name, err)
				}
			} else {
				info, err := certstate.CustomCertificateInfo(cert.Name)
				switch {
				case err == nil && info != nil && info.Fingerprint != "":
					if err := certstate.RemoveCertificateSymlinks(info.Fingerprint); err != nil {
						return fmt.Errorf("cannot remove existing symlinks for custom certificate %q: %v", cert.Name, err)
					}
				case err != nil && !errors.Is(err, os.ErrNotExist):
					return fmt.Errorf("cannot inspect custom certificate %q for cleanup: %v", cert.Name, err)
				}
			}

			// Remove the certificate file, if it exists.
			if err := certstate.RemoveCertificate(cert.Name); err != nil {
				return fmt.Errorf("cannot remove custom certificate %q: %v", cert.Name, err)
			}

			// Clear the certificate fingerprint in the config
			if err := tr.Set("core", fpKey, nil); err != nil {
				return fmt.Errorf("cannot clear fingerprint for custom certificate %q: %v", cert.Name, err)
			}
		case certstate.CertificateStateAccepted, certstate.CertificateStateBlocked:
			contentForFingerprint := cert.Content

			// If there has been no content set for the certificate in this transaction,
			// there might already be existing certificate content on disk from a previous
			// configuration.
			if !cert.HasContent {
				existingContent, err := os.ReadFile(certstate.CertificatePath(cert.Name))
				if err != nil {
					// If the file does not exist, then it means the user is trying to change some configuration
					// of a certificate without first providing the certificate content. Reflect this specifically.
					if errors.Is(err, os.ErrNotExist) {
						return fmt.Errorf("cannot update state for custom certificate %q: certificate does not exist", cert.Name)
					}
					return fmt.Errorf("cannot read existing certificate content for %q: %v", cert.Name, err)
				}
				contentForFingerprint = string(existingContent)
			}

			// Calculate the fingerprint, we need it for how we name the symlinks
			fp, err := certificateFingerprint(contentForFingerprint)
			if err != nil {
				return fmt.Errorf("cannot parse certificate content for %q: %v", cert.Name, err)
			}

			if cert.HasContent {
				// Clear any old symlinks if fingerprint changed
				if err := certstate.RemoveCertificateSymlinks(fpOld); err != nil {
					return fmt.Errorf("cannot remove existing symlinks for custom certificate %q: %v", cert.Name, err)
				}

				// Ensure new symlinks
				if err := ensureCertificateState(fp, cert); err != nil {
					return err
				}

				if err := certstate.WriteCertificate(cert.Name, cert.Content); err != nil {
					return fmt.Errorf("cannot write custom certificate %q: %v", cert.Name, err)
				}
			}

			// Update the certificate fingerprint in the config
			if err := tr.Set("core", fpKey, fp); err != nil {
				return fmt.Errorf("cannot update fingerprint for custom certificate %q: %v", cert.Name, err)
			}

		default:
			return fmt.Errorf("invalid state value for custom certificate %q: %q", cert.Name, cert.State)
		}
	}
	return certstate.GenerateCertificateDatabase()
}

func validateCustomCertificateRequest(tr RunTransaction) error {
	for _, change := range tr.Changes() {
		if !strings.HasPrefix(change, "core."+customCertPrefix) {
			continue
		}

		key := strings.TrimPrefix(change, "core.")
		name, field, err := parseCustomCertKey(key)
		if err != nil {
			return err
		}

		if name == "" && field == "" {
			if key == customCertPrefix {
				return fmt.Errorf("cannot set %q directly", key)
			}
			continue
		}

		if field == "" {
			var v any
			if err := tr.Get("core", key, &v); err != nil && !config.IsNoOption(err) {
				return err
			}
			if v != nil {
				return fmt.Errorf("cannot set %q directly", key)
			}

			// Unsetting pki.certs.custom.<name> is the supported removal path.
			continue
		}

		// retrieve the new value for the changed field and update the certificate in the map
		v, err := coreCfg(tr, key)
		if err != nil {
			return err
		}

		switch field {
		case "content":
			if v == "" {
				// empty content is allowed, as it indicates certificate removal
				continue
			}
			_, err := certstate.ParseCertificateData([]byte(v))
			if err != nil {
				return fmt.Errorf("invalid certificate content for %q: %v", key, err)
			}
		case "state":
			switch v {
			case certstate.CertificateStateAccepted, certstate.CertificateStateBlocked:
				// thats ok
			default:
				return fmt.Errorf("invalid state value for %q: %q", key, v)
			}
		case "name":
			if !validCertName(v) {
				return fmt.Errorf("invalid certificate name for %q: %q", key, v)
			}
		case "fingerprint":
			return fmt.Errorf("cannot set %q: field is read-only", key)
		default:
			return fmt.Errorf("unexpected field %q in custom certificate change", field)
		}
	}
	return nil
}

func handleGetCustomCertificates(key string) (any, error) {
	infos, err := certstate.CustomCertificates()
	if err != nil {
		return nil, err
	}

	name, _, err := parseCustomCertKey(key)
	if err != nil {
		return nil, err
	}

	if name == "" {
		// When retrieving all certificates, let us not return the content,
		// as it can be large.
		for _, info := range infos {
			info.Content = ""
		}
		return infos, nil
	}

	filtered := make(map[string]*certstate.CertificateInfo)
	for _, info := range infos {
		if info.Name == name {
			filtered[name] = info
			break
		}
	}

	return filtered, nil
}
