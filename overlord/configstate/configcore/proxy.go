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
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
)

var (
	devicestateResetSession = devicestate.ResetSession
)

var proxyConfigKeys = map[string]bool{
	"http_proxy":  true,
	"https_proxy": true,
	"ftp_proxy":   true,
	"no_proxy":    true,
}

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.proxy.http"] = true
	supportedConfigurations["core.proxy.https"] = true
	supportedConfigurations["core.proxy.ftp"] = true
	supportedConfigurations["core.proxy.no-proxy"] = true
	supportedConfigurations["core.proxy.store"] = true
}

func etcEnvironment() string {
	return filepath.Join(dirs.GlobalRootDir, "/etc/environment")
}

func updateEtcEnvironmentConfig(path string, config map[string]string) error {
	f, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	toWrite, err := updateKeyValueStream(f, proxyConfigKeys, config)
	if err != nil {
		return err
	}
	if toWrite != nil {
		// XXX: would be great to atomically write but /etc/environment
		//      is a single bind mount :/
		var buf bytes.Buffer
		for _, entry := range toWrite {
			fmt.Fprintln(&buf, entry)
		}
		return os.WriteFile(path, buf.Bytes(), 0644)
	}

	return nil
}

func handleProxyConfiguration(tr RunTransaction, opts *fsOnlyContext) error {
	config := map[string]string{}
	// normal proxy settings
	for _, key := range []string{"http", "https", "ftp"} {
		output, err := coreCfg(tr, "proxy."+key)
		if err != nil {
			return err
		}
		config[key+"_proxy"] = output
	}
	// handle no_proxy
	output, err := coreCfg(tr, "proxy.no-proxy")
	if err != nil {
		return err
	}
	config["no_proxy"] = output

	if err := updateEtcEnvironmentConfig(etcEnvironment(), config); err != nil {
		return err
	}

	return nil
}

func validateProxyStore(tr RunTransaction) error {
	proxyStore, err := coreCfg(tr, "proxy.store")
	if err != nil {
		return err
	}

	if proxyStore == "" {
		return nil
	}

	st := tr.State()
	st.Lock()
	defer st.Unlock()

	store, err := assertstate.Store(st, proxyStore)
	if errors.Is(err, &asserts.NotFoundError{}) {
		return fmt.Errorf("cannot set proxy.store to %q without a matching store assertion", proxyStore)
	}
	if err == nil && store.URL() == nil {
		return fmt.Errorf("cannot set proxy.store to %q with a matching store assertion with url unset", proxyStore)
	}
	return err
}

func handleProxyStore(tr RunTransaction, opts *fsOnlyContext) error {
	// is proxy.store being modififed?
	proxyStoreInChanges := false
	for _, name := range tr.Changes() {
		if name == "core.proxy.store" {
			proxyStoreInChanges = true
			break
		}
	}
	if !proxyStoreInChanges {
		return nil
	}

	proxyStore, err := coreCfg(tr, "proxy.store")
	if err != nil {
		return err
	}
	var prevProxyStore string
	if err := tr.GetPristine("core", "proxy.store", &prevProxyStore); err != nil && !config.IsNoOption(err) {
		return err
	}
	if proxyStore != prevProxyStore {
		// XXX ideally we should do this only when committing but we
		// don't have infrastructure for that ATM, it just means the
		// store will have to recreate the session.
		// XXX the store code doesn't acquire the store ids and the
		// session together atomically, this can be fixed only in a
		// larger cleanup of how store.DeviceAndAuthContext
		// operates. Hopefully it is atypical to set proxy.store while
		// non-automatic store operations are happening, this approach
		// is a best-effort for now.
		state := tr.State()
		state.Lock()
		defer state.Unlock()
		if err := devicestateResetSession(state); err != nil {
			return err
		}
	}
	return nil
}
