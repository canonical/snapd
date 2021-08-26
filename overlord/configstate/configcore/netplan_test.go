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

package configcore_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dbusutil/dbustest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type netplanSuite struct {
	configcoreSuite
}

var _ = Suite(&netplanSuite{})

var mockNetplanConfigYaml = `
network:
  renderer: NetworkManager
  version: 2
`

func makeDBusMethodReplyHeader(msg *dbus.Message, responseSig dbus.Signature) map[dbus.HeaderField]dbus.Variant {
	return map[dbus.HeaderField]dbus.Variant{
		dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
		dbus.FieldSender:      dbus.MakeVariant(":1"),
		dbus.FieldSignature:   dbus.MakeVariant(responseSig),
	}
}

func fakeDBusConnection(msg *dbus.Message, n int) ([]*dbus.Message, error) {
	if msg.Type != dbus.TypeMethodCall {
		return nil, fmt.Errorf("unexpected msg to fakeDBusConnection: %v", msg)
	}

	// Config()
	if msg.Headers[dbus.FieldPath] == dbus.MakeVariant(dbus.ObjectPath("/io/netplan/Netplan")) &&
		msg.Headers[dbus.FieldInterface] == dbus.MakeVariant("io.netplan.Netplan") &&
		msg.Headers[dbus.FieldMember] == dbus.MakeVariant("Config") {
		responseSig := dbus.SignatureOf(dbus.ObjectPath(""))
		return []*dbus.Message{
			{
				Type:    dbus.TypeMethodReply,
				Headers: makeDBusMethodReplyHeader(msg, responseSig),
				Body:    []interface{}{dbus.ObjectPath("/io/netplan/Netplan/config/WFIU80")},
			},
		}, nil

	}

	// Get()
	if msg.Headers[dbus.FieldPath] == dbus.MakeVariant(dbus.ObjectPath("/io/netplan/Netplan/config/WFIU80")) &&
		msg.Headers[dbus.FieldInterface] == dbus.MakeVariant("io.netplan.Netplan.Config") &&
		msg.Headers[dbus.FieldMember] == dbus.MakeVariant("Get") {
		responseSig := dbus.SignatureOf("")
		return []*dbus.Message{
			{
				Type:    dbus.TypeMethodReply,
				Headers: makeDBusMethodReplyHeader(msg, responseSig),
				Body:    []interface{}{mockNetplanConfigYaml},
			},
		}, nil

	}

	return nil, fmt.Errorf("unexpected message %v", msg)

}

func (s *netplanSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, IsNil)
}

func (s *netplanSuite) TestNetplanGetFromDbusHappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	conn, err := dbustest.Connection(fakeDBusConnection)
	c.Assert(err, IsNil)
	restore := dbusutil.MockOnlySystemBusAvailable(conn)
	defer restore()

	tr := config.NewTransaction(s.state)

	// full doc
	netplanCfg := make(map[string]interface{})
	err = tr.Get("core", "system.network.netplan", &netplanCfg)
	c.Assert(err, IsNil)
	c.Check(netplanCfg, DeepEquals, map[string]interface{}{
		"network": map[string]interface{}{
			"renderer": "NetworkManager",
			"version":  json.Number("2"),
		},
	})

	// only the "network" subset
	netplanCfg = make(map[string]interface{})
	err = tr.Get("core", "system.network.netplan.network", &netplanCfg)
	c.Assert(err, IsNil)
	c.Check(netplanCfg, DeepEquals, map[string]interface{}{
		"renderer": "NetworkManager",
		"version":  json.Number("2"),
	})

	// only the "network.version" subset
	var ver json.Number
	err = tr.Get("core", "system.network.netplan.network.version", &ver)
	c.Assert(err, IsNil)
	c.Check(ver, Equals, json.Number("2"))
}

func (s *netplanSuite) TestNetplanGetFromDbusError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	conn, err := dbustest.Connection(fakeDBusConnection)
	c.Assert(err, IsNil)
	restore := dbusutil.MockOnlySystemBusAvailable(conn)
	defer restore()

	tr := config.NewTransaction(s.state)

	// no subkey in map
	var str string
	err = tr.Get("core", "system.network.netplan.xxx", &str)
	c.Assert(err, ErrorMatches, `snap "core" has no "system.network.netplan.xxx" configuration option`)
}

func (s *netplanSuite) TestNetplanReadOnlyForNow(c *C) {
	err := configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"system.network.netplan.network.renderer": "networkd",
		},
	})
	c.Assert(err, ErrorMatches, "cannot set netplan config yet")
}
