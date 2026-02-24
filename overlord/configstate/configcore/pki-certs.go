// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

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
	if !strings.HasPrefix(key, customCertPrefix+".") {
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

		// we only care about changes to fields, not the bare object
		if name == "" || field == "" {
			continue
		}

		// ensure the certificate is in the map so that we can apply all changes to it together later
		var cert certificate
		if existing, ok := certs[name]; ok {
			cert = existing
		} else {
			cert = certificate{Name: name}
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

		// if content was explicitly set to empty, remove the certificate itself
		if cert.HasContent && cert.Content == "" {
			if err := certstate.RemoveCertificate(cert.Name); err != nil {
				return fmt.Errorf("cannot remove custom certificate %q: %v", cert.Name, err)
			}
		}

		if fpOld != "" {
			if err := certstate.RemoveCertificateSymlinks(fpOld); err != nil {
				return fmt.Errorf("cannot remove existing symlinks for custom certificate %q: %v", cert.Name, err)
			}
		}

		// If state is unset, ignore
		// Clear the fingerprint if the state is unset, as for that case the
		// certificate will be unavailable.
		if cert.State == certstate.CertificateStateUnset && fpOld != "" {
			if err := tr.Set("core", fpKey, ""); err != nil {
				return fmt.Errorf("cannot clear fingerprint for custom certificate %q: %v", cert.Name, err)
			}
			continue
		}

		contentForFingerprint := cert.Content
		if !cert.HasContent {
			existingContent, err := os.ReadFile(certstate.CertificatePath(cert.Name))
			if err != nil {
				return fmt.Errorf("cannot read existing certificate content for %q: %v", cert.Name, err)
			}
			contentForFingerprint = string(existingContent)
		}

		// Calculate the new fingerprint, we need it for how we name the symlinks
		fp, err := certificateFingerprint(contentForFingerprint)
		if err != nil {
			return fmt.Errorf("cannot parse certificate content for %q: %v", cert.Name, err)
		}

		if cert.HasContent {
			if err := certstate.WriteCertificate(cert.Name, cert.Content); err != nil {
				return fmt.Errorf("cannot write custom certificate %q: %v", cert.Name, err)
			}
		}

		if err := certstate.RemoveCertificateSymlinks(fp); err != nil {
			return fmt.Errorf("cannot remove existing symlinks for custom certificate %q: %v", cert.Name, err)
		}

		if err := certstate.SetCertificateState(cert.Name, fp, cert.State); err != nil {
			return fmt.Errorf("cannot set state for custom certificate %q: %v", cert.Name, err)
		}

		// Update the certificate fingerprint in the config
		if err := tr.Set("core", fpKey, fp); err != nil {
			return fmt.Errorf("cannot update fingerprint for custom certificate %q: %v", cert.Name, err)
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

		// we only care about changes to fields, not the bare object
		if name == "" || field == "" {
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
			case certstate.CertificateStateUnset, certstate.CertificateStateAccepted, certstate.CertificateStateBlocked:
				// thats ok
			default:
				return fmt.Errorf("invalid state value for %q: %q", key, v)
			}
		case "name":
			if !validCertName(v) {
				return fmt.Errorf("invalid certificate name for %q: %q", key, v)
			}
		default:
			return fmt.Errorf("unexpected field %q in custom certificate change", field)
		}
	}
	return nil
}

func handleGetCustomCertificates(key string) (any, error) {
	return certstate.CustomCertificates()
}
