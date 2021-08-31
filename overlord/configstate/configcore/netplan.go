// -*- Mode: Go; indent-tabs-mode: t -*-

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

import (
	"fmt"
	"strings"

	// TODO: Move to yaml.v3 everywhere, there is PR#10696 that starts
	//       this. However it is not trivial yaml.v2 accepts duplicated
	//       keys in maps and v3 does not. There might be snaps in the
	//       wild that we could break by going to v3.
	//
	// Move this part of the code to yaml.v3 because without it we run
	// into incompatibilites of maps between json and yaml:
	// "json: unsupported type: map[interface {}]interface {}" because
	// because yaml.v2 unmarshalls by default to "map[string]interface{}"
	// v3 fixes this, see https://github.com/go-yaml/yaml/pull/385#issuecomment-475588596
	"gopkg.in/yaml.v3"

	"github.com/godbus/dbus"
	"github.com/godbus/dbus/introspect"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/release"
)

func init() {
	// add supported configuration of this module
	supportedConfigurations["core.system.network.netplan"] = true
	// and register as exteranl config
	config.RegisterExternalConfig("core", "system.network.netplan", getNetplanFromSystem)
}

func validateNetplanSettings(tr config.Conf) error {
	// XXX: validate somehow once we support writing?
	return nil
}

func handleNetplanConfiguration(tr config.Conf, opts *fsOnlyContext) error {
	for _, chg := range tr.Changes() {
		if strings.HasPrefix(chg, "core.system.network.netplan.") {
			return fmt.Errorf("cannot set netplan config yet")
		}
	}

	return nil
}

func hasDBusMethodOnInterface(node *introspect.Node, ifName, methodName string) bool {
	for _, iff := range node.Interfaces {
		if iff.Name == ifName {
			for _, mth := range iff.Methods {
				if mth.Name == methodName {
					return true
				}
			}
		}
	}
	return false
}

func getNetplanFromSystem(key string) (result interface{}, err error) {
	if release.OnClassic {
		return nil, nil
	}

	conn, err := dbusutil.SystemBus()
	if err != nil {
		return nil, err
	}
	// godbus uses a global systemBus object internally so we *must*
	// not close the connection.

	var netplanConfigSnapshotBusAddr string
	netplan := conn.Object("io.netplan.Netplan", "/io/netplan/Netplan")

	// introspect
	node, err := introspect.Call(netplan)
	if derr, ok := err.(dbus.Error); ok {
		// ignore if there is no dbus service for netplan
		if derr.Name == "org.freedesktop.DBus.Error.ServiceUnknown" {
			return nil, nil
		}
	}
	// XXX: should we log and return nil here to avoid breaking the
	// conf system if something is wrong with dbus on the machine?
	if err != nil {
		return nil, err
	}
	if !hasDBusMethodOnInterface(node, "io.netplan.Netplan", "Config") {
		return nil, nil
	}

	if err := netplan.Call("io.netplan.Netplan.Config", 0).Store(&netplanConfigSnapshotBusAddr); err != nil {
		return nil, err
	}

	var netplanYamlCfg string
	netplanCfgSnapshot := conn.Object("io.netplan.Netplan", dbus.ObjectPath(netplanConfigSnapshotBusAddr))
	if err := netplanCfgSnapshot.Call("io.netplan.Netplan.Config.Get", 0).Store(&netplanYamlCfg); err != nil {
		return nil, err
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(netplanYamlCfg), &cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
