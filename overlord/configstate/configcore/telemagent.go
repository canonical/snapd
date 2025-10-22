// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2017-2022 Canonical Ltd
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
	"encoding/pem"
	"errors"
	"net/url"
	"os"

	"github.com/snapcore/snapd/overlord/restart"
)

func init() {
	supportedConfigurations["core.telemagent.telemgw-url"] = true
	supportedConfigurations["core.telemagent.ca-cert"] = true
	supportedConfigurations["core.telemagent.endpoint"] = true
	supportedConfigurations["core.telemagent.port"] = true
	supportedConfigurations["core.telemagent.email"] = true
}

func validateTelemAgentConf(tr RunTransaction) error {

	sslPath, err := coreCfg(tr, "telemagent.ca-cert")
	if err != nil {
		return err
	}

	// check cert

	data, err := os.ReadFile(sslPath)
	if err != nil {
		return err
	}

	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return errors.New("not a PEM certificate")
	}
	_, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}

	//check endpoint
	endpoint, err := coreCfg(tr, "telemagent.endpoint")
	if err != nil {
		return err
	}
	_, err = url.Parse(endpoint)
	if err != nil {
		return err
	}

	//check url
	u, err := coreCfg(tr, "telemagent.telemgw-url")
	if err != nil {
		return err
	}
	_, err = url.Parse(u)
	if err != nil {
		return err
	}

	return nil
}

func handleTelemAgentConfiguration(tr RunTransaction, opts *fsOnlyContext) error {
	st := tr.State()

	st.Lock()
	defer st.Unlock()

	restartRequest(st, restart.RestartDaemon, nil)

	return nil

}
