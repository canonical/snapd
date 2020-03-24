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
	"reflect"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/release"
)

var (
	Stdout = os.Stdout
	Stderr = os.Stderr
)

// coreCfg returns the configuration value for the core snap.
func coreCfg(tr config.ConfGetter, key string) (result string, err error) {
	var v interface{} = ""
	if err := tr.Get("core", key, &v); err != nil && !config.IsNoOption(err) {
		return "", err
	}
	// TODO: we could have a fully typed approach but at the
	// moment we also always use "" to mean unset as well, this is
	// the smallest change
	return fmt.Sprintf("%v", v), nil
}

// supportedConfigurations contains a set of handled configuration keys.
// The actual values are populated by `init()` functions in each module.
var supportedConfigurations = make(map[string]bool, 32)

func validateBoolFlag(tr config.ConfGetter, flag string) error {
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

// PlainCoreConfig carries a read-only copy of core config and implements
// config.ConfGetter interface.
type PlainCoreConfig map[string]interface{}

// Get implements config.ConfGetter interface.
func (cfg PlainCoreConfig) Get(snapName, key string, result interface{}) error {
	if snapName != "core" {
		return fmt.Errorf("internal error: expected core snap in Get(), %q was requested", snapName)
	}

	val, ok := cfg[key]
	if !ok {
		return &config.NoOptionError{SnapName: snapName, Key: key}
	}

	rv := reflect.ValueOf(result)
	rv.Elem().Set(reflect.ValueOf(val))
	return nil
}

// GetMaybe implements config.ConfGetter interface.
func (cfg PlainCoreConfig) GetMaybe(instanceName, key string, result interface{}) error {
	err := cfg.Get(instanceName, key, result)
	if err != nil && !config.IsNoOption(err) {
		return err
	}
	return nil
}

// fsOnlyContext encapsulates extra options passed to individual core config
// handlers when configuration is applied to a specific root directory with
// FilesystemOnlyApply().
type fsOnlyContext struct {
	RootDir string
}

// FilesystemOnlyApply applies filesystem modifications under rootDir, according to the
// cfg configuration. This is a subset of core config options that is important
// early during boot, before all the configuration is applied as part of
// normal execution of configure hook.
func FilesystemOnlyApply(rootDir string, cfg config.ConfGetter) error {
	if rootDir == "" {
		return fmt.Errorf("internal error: root directory for configcore.FilesystemOnlyApply() not set")
	}

	opts := &fsOnlyContext{RootDir: rootDir}

	if err := validateExperimentalSettings(cfg); err != nil {
		return err
	}
	if err := validateWatchdogOptions(cfg); err != nil {
		return err
	}
	if err := validateNetworkSettings(cfg); err != nil {
		return err
	}

	// Export experimental.* flags to a place easily accessible from snapd helpers.
	if err := doExportExperimentalFlags(cfg, opts); err != nil {
		return err
	}

	// see if it makes sense to run at all
	if release.OnClassic {
		// nothing to do
		return nil
	}

	// handle some of the core config options:
	// service.*.disable
	if err := handleServiceDisableConfiguration(cfg, opts); err != nil {
		return err
	}
	// system.power-key-action
	if err := handlePowerButtonConfiguration(cfg, opts); err != nil {
		return err
	}
	// pi-config.*
	if err := handlePiConfiguration(cfg, opts); err != nil {
		return err
	}
	// watchdog.{runtime-timeout,shutdown-timeout}
	if err := handleWatchdogConfiguration(cfg, opts); err != nil {
		return err
	}
	// network.disable-ipv6
	if err := handleNetworkConfiguration(cfg, opts); err != nil {
		return err
	}

	return nil
}

func Run(tr config.Conf) error {
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
	if err := validateRefreshRateLimit(tr); err != nil {
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
	if err := validateAutomaticSnapshotsExpiration(tr); err != nil {
		return err
	}
	// FIXME: ensure the user cannot set "core seed.loaded"

	// capture cloud information
	if err := setCloudInfoWhenSeeding(tr); err != nil {
		return err
	}

	// Export experimental.* flags to a place easily accessible from snapd helpers.
	if err := doExportExperimentalFlags(tr, nil); err != nil {
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
	if err := handleServiceDisableConfiguration(tr, nil); err != nil {
		return err
	}
	// system.power-key-action
	if err := handlePowerButtonConfiguration(tr, nil); err != nil {
		return err
	}
	// pi-config.*
	if err := handlePiConfiguration(tr, nil); err != nil {
		return err
	}
	// proxy.{http,https,ftp}
	if err := handleProxyConfiguration(tr); err != nil {
		return err
	}
	// watchdog.{runtime-timeout,shutdown-timeout}
	if err := handleWatchdogConfiguration(tr, nil); err != nil {
		return err
	}
	// network.disable-ipv6
	if err := handleNetworkConfiguration(tr, nil); err != nil {
		return err
	}

	return nil
}
