// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
)

func handleCertConfiguration(tr config.Conf) error {
	for _, name := range tr.Changes() {
		if !strings.HasPrefix(name, "core.certs.") {
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
				return fmt.Errorf("cannot remove cert: %v", err)
			}
		default:
			if err := os.MkdirAll(dirs.SnapdStoreSSLCertsDir, 0755); err != nil {
				return fmt.Errorf("cannot create store ssl certs dir: %v", err)
			}
			if err := ioutil.WriteFile(certPath, []byte(cert), 0644); err != nil {
				return fmt.Errorf("cannot write extra cert: %v", err)
			}
		}
	}

	return nil
}

func validateCertSettings(tr config.Conf) error {
	for _, name := range tr.Changes() {
		if !strings.HasPrefix(name, "core.certs.") {
			continue
		}

		nameWithoutSnap := strings.SplitN(name, ".", 2)[1]
		cert, err := coreCfg(tr, nameWithoutSnap)
		if err != nil {
			return fmt.Errorf("internal error: cannot get data for %s: %v", nameWithoutSnap, err)
		}
		if cert != "" {
			optionName := strings.SplitN(name, ".", 3)[2]
			block, rest := pem.Decode([]byte(cert))
			if block == nil || block.Type != "CERTIFICATE" || len(rest) > 0 {
				return fmt.Errorf("cannot decode pem certificate %q", optionName)
			}
			// XXX: add more validations?
		}
	}

	return nil
}
