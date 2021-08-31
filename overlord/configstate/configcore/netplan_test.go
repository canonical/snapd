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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/configstate/configcore/netplantest"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type netplanSuite struct {
	configcoreSuite
	testutil.DBusTest

	backend *netplantest.NetplanServer
}

var _ = Suite(&netplanSuite{})

var mockNetplanConfigYaml = `
network:
  renderer: NetworkManager
  version: 2
`

func (s *netplanSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	s.DBusTest.SetUpTest(c)

	backend, err := netplantest.NewNetplanServer(mockNetplanConfigYaml)
	c.Assert(err, IsNil)
	s.AddCleanup(func() { c.Check(backend.Stop(), IsNil) })
	s.backend = backend

	// We mock the system bus with a private session bus, that is
	// good enough for the unit tests. Spread tests cover the real
	// bus.
	restore := dbusutil.MockOnlySystemBusAvailable(s.SessionBus)
	s.AddCleanup(restore)

	restore = release.MockOnClassic(false)
	s.AddCleanup(restore)

	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, IsNil)
}

func (s *netplanSuite) TearDownTest(c *C) {
	s.configcoreSuite.TearDownTest(c)
	s.DBusTest.TearDownTest(c)
}

func (s *netplanSuite) TestNetplanGetFromDbusNoSuchService(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Note that we do not export any netplan dbus api here

	tr := config.NewTransaction(s.state)
	netplanCfg := make(map[string]interface{})
	err := tr.Get("core", "system.network.netplan", &netplanCfg)
	c.Assert(err, ErrorMatches, `snap "core" has no "system.network.netplan" configuration option`)
}

func (s *netplanSuite) TestNetplanGetFromDbusNoV2Api(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// export the V1 api only (no config support with that)
	s.backend.ExportApiV1()

	tr := config.NewTransaction(s.state)

	// no netplan configuration with the "v1" netplan api
	var str string
	err := tr.Get("core", "system.network.netplan", &str)
	c.Assert(err, ErrorMatches, `snap "core" has no "system.network.netplan" configuration option`)
}

func (s *netplanSuite) TestNetplanGetNoSupportOnClassic(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := release.MockOnClassic(true)
	s.AddCleanup(restore)

	// export the V2 api, things work with that
	s.backend.ExportApiV2()

	tr := config.NewTransaction(s.state)
	netplanCfg := make(map[string]interface{})
	err := tr.Get("core", "system.network.netplan", &netplanCfg)
	c.Assert(err, ErrorMatches, `snap "core" has no "system.network.netplan" configuration option`)
}

func (s *netplanSuite) TestNetplanGetFromDbusHappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// export the V2 api, things work with that
	s.backend.ExportApiV2()

	tr := config.NewTransaction(s.state)

	// full doc
	netplanCfg := make(map[string]interface{})
	err := tr.Get("core", "system.network.netplan", &netplanCfg)
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

func (s *netplanSuite) TestNetplanGetFromDbusNoSuchConfigError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// export the V2 api, things work with that
	s.backend.ExportApiV2()

	tr := config.NewTransaction(s.state)

	// no subkey in in our yaml configuration like that, we get an
	// expected error from the config mechanism
	var str string
	err := tr.Get("core", "system.network.netplan.xxx", &str)
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
