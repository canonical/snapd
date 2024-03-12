// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2021 Canonical Ltd
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

// TODO: Move to yaml.v3 everywhere, there is PR#10696 that starts
//       this. However it is not trivial yaml.v2 accepts duplicated
//       keys in maps and v3 does not. There might be snaps in the
//       wild that we could break by going to v3.
//
// Move this part of the code to yaml.v3 because without it we run
// into incompatibilites of maps between json and yaml:
// "json: unsupported type: map[interface{}]interface{}" because
// because yaml.v2 unmarshalls by default to "map[interface{}]interface{}"
// v3 fixes this, see https://github.com/go-yaml/yaml/pull/385#issuecomment-475588596
import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/godbus/dbus"
	"gopkg.in/yaml.v3"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.system.network.netplan"] = true
	// and register as external config
	config.RegisterExternalConfig("core", "system.network.netplan", getNetplanFromSystem)
}

type connectivityCheckStore interface {
	ConnectivityCheck() (map[string]bool, error)
}

var snapstateStore = func(st *state.State, deviceCtx snapstate.DeviceContext) connectivityCheckStore {
	return snapstate.Store(st, deviceCtx)
}

func storeReachable(st *state.State) error {
	st.Lock()
	sto := snapstateStore(st, nil)
	st.Unlock()
	status, err := sto.ConnectivityCheck()
	if err != nil {
		return err
	}

	var unreachableHost []string
	for host, reachable := range status {
		if !reachable {
			unreachableHost = append(unreachableHost, host)
		}
	}
	if len(unreachableHost) > 0 {
		sort.Strings(unreachableHost)
		logger.Debugf("unreachable store hosts: %v", unreachableHost)
		return fmt.Errorf("cannot connect to %q", strings.Join(unreachableHost, ","))
	}

	return nil
}

func isNoServiceOrMethodErr(err error) bool {
	derr, ok := err.(dbus.Error)
	if !ok {
		return false
	}

	switch derr.Name {
	case "org.freedesktop.DBus.Error.ServiceUnknown":
		fallthrough
	case "org.freedesktop.DBus.Error.UnknownInterface":
		fallthrough
	case "org.freedesktop.DBus.Error.UnknownMethod":
		return true
	}
	return false
}

func getNetplanCfgSnapshot() (dbus.BusObject, error) {
	conn, err := dbusutil.SystemBus()
	if err != nil {
		return nil, err
	}
	// godbus uses a global systemBus object internally so we *must*
	// not close the connection.

	var netplanConfigSnapshotBusAddr dbus.ObjectPath
	netplan := conn.Object("io.netplan.Netplan", "/io/netplan/Netplan")

	if err := netplan.Call("io.netplan.Netplan.Config", 0).Store(&netplanConfigSnapshotBusAddr); err != nil {
		return nil, err
	}
	logger.Debugf("using netplan config snapshot %v", netplanConfigSnapshotBusAddr)

	netplanCfgSnapshot := conn.Object("io.netplan.Netplan", dbus.ObjectPath(netplanConfigSnapshotBusAddr))
	return netplanCfgSnapshot, nil
}

func validateNetplanSettings(tr RunTransaction) error {
	// validation is done by netplan itself on apply, there is no
	// way to dry-run this
	return nil
}

func isNetplanChange(chg string) bool {
	return chg == "core.system.network.netplan" || strings.HasPrefix(chg, "core.system.network.netplan.")
}

func hasNetplanChanges(tr RunTransaction) bool {
	for _, chg := range tr.Changes() {
		if isNetplanChange(chg) {
			return true
		}
	}
	return false
}

var storeReachableRetryWait = 1 * time.Second

func testStoreReachableWithRetry(state *state.State, n int, wait time.Duration) (int, bool) {
	for i := 0; i < n; i++ {
		if err := storeReachable(state); err == nil {
			return i, true
		}
		time.Sleep(wait)
	}
	return n, false
}

func handleNetplanConfiguration(tr RunTransaction, opts *fsOnlyContext) (err error) {
	if !hasNetplanChanges(tr) {
		return nil
	}

	var cfg map[string]interface{}
	if err := tr.Get("core", "system.network.netplan", &cfg); err != nil && !config.IsNoOption(err) {
		return fmt.Errorf("cannot get netpan config: %v", err)
	}

	netplanCfgSnapshot, err := getNetplanCfgSnapshot()
	// Having no netplan config is *not* an error, we just
	// do not support netplan config.
	if isNoServiceOrMethodErr(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if e := cancelNetplanCfgSnapshot(netplanCfgSnapshot); e != nil {
				err = fmt.Errorf("%s and %s", err, e)
			}
		}
	}()

	seeded, err := alreadySeeded(tr)
	if err != nil {
		return err
	}

	originHint := "90-snapd-config"
	if !seeded {
		// This is the origin used by the defaults settings established from
		// the base, and it is also modified by console-conf if it runs. We
		// really want to clean it if console-conf is disabled, as we want to
		// override the base defaults. If console conf is enabled, it will
		// rewrite the file anyway setting its own thing, which is fine, as it
		// is a manual configuration and the defaults might even conflict with
		// it.
		originHint = "00-snapd-config"
	}

	// Always starts with a clean config to avoid merging of keys
	// that got unset.
	configs := []string{"network=null"}
	// and then pass the full new config in
	for key := range cfg {
		// We pass the new config back to netplan as json, the reason
		// is that the dbus api accepts only a single line string, see
		// see https://github.com/canonical/netplan/pull/210
		jsonNetplanConfigRaw, err := json.Marshal(cfg[key])
		if err != nil {
			return fmt.Errorf("cannot marshal netplan config: %v", err)
		}
		configs = append(configs, fmt.Sprintf("%s=%s", key, string(jsonNetplanConfigRaw)))
	}

	// now apply
	for _, jsonNetplanConfig := range configs {
		var wasSet bool
		if err := netplanCfgSnapshot.Call("io.netplan.Netplan.Config.Set", 0, jsonNetplanConfig, originHint).Store(&wasSet); err != nil {
			return fmt.Errorf("cannot set netplan config: %v", err)
		}
		if !wasSet {
			return fmt.Errorf("cannot set netplan config: no specific reason returned from netplan")
		}
	}

	// re-try reaching the store to guard against flaky networks
	tries, storeReachableBefore := testStoreReachableWithRetry(tr.State(), 5, storeReachableRetryWait)
	logger.Debugf("store reachable before netplan changes: %v (tried %v times)", storeReachableBefore, tries)

	var wasTried bool
	timeoutInSeconds := 30
	if err := netplanCfgSnapshot.Call("io.netplan.Netplan.Config.Try", 0, uint32(timeoutInSeconds)).Store(&wasTried); err != nil {
		return fmt.Errorf("cannot try netplan config: %v", err)
	}
	if !wasTried {
		return fmt.Errorf("cannot try netplan config: no specific reason returned from netplan")
	}

	tries, storeReachableAfter := testStoreReachableWithRetry(tr.State(), 5, storeReachableRetryWait)
	logger.Debugf("store reachable after netplan changes: %v (tried %v times)", storeReachableAfter, tries)

	if storeReachableBefore && !storeReachableAfter {
		return fmt.Errorf("cannot set netplan config: store no longer reachable")
	}

	var wasApplied bool
	if err := netplanCfgSnapshot.Call("io.netplan.Netplan.Config.Apply", 0).Store(&wasApplied); err != nil {
		return fmt.Errorf("cannot apply netplan config: %v", err)
	}
	if !wasApplied {
		return fmt.Errorf("cannot apply netplan config: no specific reason returned from netplan")
	}
	logger.Debugf("netplan config applied correctly")

	return nil
}

func getNetplanFromSystem(key string) (result interface{}, err error) {
	if release.OnClassic {
		return nil, nil
	}

	netplanCfgSnapshot, err := getNetplanCfgSnapshot()
	// Having no netplan config is *not* an error, we just
	// do not support netplan config.
	if isNoServiceOrMethodErr(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var netplanYamlCfg string
	if err := netplanCfgSnapshot.Call("io.netplan.Netplan.Config.Get", 0).Store(&netplanYamlCfg); err != nil {
		return nil, err
	}
	defer func() {
		if err := cancelNetplanCfgSnapshot(netplanCfgSnapshot); err != nil {
			logger.Noticef("%s", err)
		}
	}()

	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(netplanYamlCfg), &cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func cancelNetplanCfgSnapshot(netplanCfgSnapshot dbus.BusObject) error {
	// and discard the config snapshot
	var wasCancelled bool
	if err := netplanCfgSnapshot.Call("io.netplan.Netplan.Config.Cancel", 0).Store(&wasCancelled); err != nil {
		return fmt.Errorf("cannot cancel netplan config: %v", err)
	}
	if !wasCancelled {
		return fmt.Errorf("cannot cancel netplan config: no specific reason returned from netplan")
	}
	return nil
}
