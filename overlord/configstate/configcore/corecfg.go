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

package configcore

import (
	"fmt"
	"os"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

var (
	Stdout = os.Stdout
	Stderr = os.Stderr
)

type Conf interface {
	Get(snapName, key string, result interface{}) error
	Set(snapName, key string, value interface{}) error
	Changes() []string
	State() *state.State
}

func coreCfg(tr Conf, key string) (result string, err error) {
	var v interface{} = ""
	if err := tr.Get("core", key, &v); err != nil && !config.IsNoOption(err) {
		return "", err
	}
	// TODO: we could have a fully typed approach but at the
	// moment we also always use "" to mean unset as well, this is
	// the smallest change
	return fmt.Sprintf("%v", v), nil
}

// supportedConfigurations will be filled in by the files (like proxy.go)
// that handle this configuration.
var supportedConfigurations = map[string]bool{
	"core.experimental.layouts":            true,
	"core.experimental.parallel-instances": true,
	"core.experimental.hotplug":            true,
}

func validateBoolFlag(tr Conf, flag string) error {
	value, err := coreCfg(tr, flag)
	if err != nil {
		return err
	}
	switch value {
	case "", "true", "false":
		// noop
	default:
		return fmt.Errorf("%s can only be set to 'true' or 'false'", flag)
	}
	return nil
}

func validateExperimentalSettings(tr Conf) error {
	if err := validateBoolFlag(tr, "experimental.layouts"); err != nil {
		return err
	}
	if err := validateBoolFlag(tr, "experimental.parallel-instances"); err != nil {
		return err
	}
	if err := validateBoolFlag(tr, "experimental.hotplug"); err != nil {
		return err
	}
	return nil
}

func Run(tr Conf) error {
	// check if the changes
	for _, k := range tr.Changes() {
		if !supportedConfigurations[k] {
			return fmt.Errorf("cannot set %q: unsupported system option", k)
		}
	}

	if err := validateProxyStore(tr); err != nil {
		return err
	}
	if err := validateRefreshSchedule(tr); err != nil {
		return err
	}
	if err := validateExperimentalSettings(tr); err != nil {
		return err
	}
	if err := validateWatchdogOptions(tr); err != nil {
		return err
	}
	if err := validateNetworkSettings(tr); err != nil {
		return err
	}
	// FIXME: ensure the user cannot set "core seed.loaded"

	// capture cloud information
	if err := setCloudInfoWhenSeeding(tr); err != nil {
		return err
	}

	// see if it makes sense to run at all
	if release.OnClassic {
		// nothing to do
		return nil
	}
	// TODO: consider allowing some of these on classic too?
	// consider erroring on core-only options on classic?

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
	// watchdog.{runtime-timeout,shutdown-timeout}
	if err := handleWatchdogConfiguration(tr); err != nil {
		return err
	}
	// network.disable-ipv6
	if err := handleNetworkConfiguration(tr); err != nil {
		return err
	}

	return nil
}
