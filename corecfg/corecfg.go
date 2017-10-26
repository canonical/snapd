// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package corecfg

import (
	"os"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/release"
)

var (
	Stdout = os.Stdout
	Stderr = os.Stderr
)

type Conf interface {
	Get(snapName, key string, result interface{}) error
}

func snapctlGet(tr Conf, key string) (string, error) {
	snapName := "core"

	var result string
	if err := tr.Get(snapName, key, &result); err != nil && !config.IsNoOption(err) {
		return "", err
	}
	return result, nil
}

func Run(tr Conf) error {
	// see if it makes sense to run at all
	if release.OnClassic {
		// nothing to do
		return nil
	}

	// handle the various core config options:
	// service.*.disable
	if err := handleServiceDisableConfiguration(tr); err != nil {
		return err
	}
	// system.power-key-action
	if err := handlePowerButtonConfiguration(tr); err != nil {
		return err
	}
	// pi-config.*
	if err := handlePiConfiguration(tr); err != nil {
		return err
	}
	// proxy.{http,https,ftp}
	if err := handleProxyConfiguration(tr); err != nil {
		return err
	}

	return nil
}
