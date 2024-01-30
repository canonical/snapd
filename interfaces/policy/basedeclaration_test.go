// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package policy_test

import (
	"fmt"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/interfaces/policy"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
)

type baseDeclSuite struct {
	baseDecl        *asserts.BaseDeclaration
	restoreSanitize func()
}

var _ = Suite(&baseDeclSuite{})

func (s *baseDeclSuite) SetUpSuite(c *C) {
	s.restoreSanitize = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	s.baseDecl = asserts.BuiltinBaseDeclaration()
}

func (s *baseDeclSuite) TearDownSuite(c *C) {
	s.restoreSanitize()
}

func (s *baseDeclSuite) connectCand(c *C, iface, slotYaml, plugYaml string) *policy.ConnectCandidate {
	if slotYaml == "" {
		slotYaml = fmt.Sprintf(`name: slot-snap
version: 0
slots:
  %s:
`, iface)
	}
	if plugYaml == "" {
		plugYaml = fmt.Sprintf(`name: plug-snap
version: 0
plugs:
  %s:
`, iface)
	}
	slotSnap := snaptest.MockInfo(c, slotYaml, nil)
	plugSnap := snaptest.MockInfo(c, plugYaml, nil)
	return &policy.ConnectCandidate{
		Plug:            interfaces.NewConnectedPlug(plugSnap.Plugs[iface], nil, nil),
		Slot:            interfaces.NewConnectedSlot(slotSnap.Slots[iface], nil, nil),
		BaseDeclaration: s.baseDecl,
	}
}

func (s *baseDeclSuite) installSlotCand(c *C, iface string, snapType snap.Type, yaml string) *policy.InstallCandidate {
	if yaml == "" {
		yaml = fmt.Sprintf(`name: install-slot-snap
version: 0
type: %s
slots:
  %s:
`, snapType, iface)
	}
	snap := snaptest.MockInfo(c, yaml, nil)
	return &policy.InstallCandidate{
		Snap:            snap,
		BaseDeclaration: s.baseDecl,
	}
}

func (s *baseDeclSuite) installPlugCand(c *C, iface string, snapType snap.Type, yaml string) *policy.InstallCandidate {
	if yaml == "" {
		yaml = fmt.Sprintf(`name: install-plug-snap
version: 0
type: %s
plugs:
  %s:
`, snapType, iface)
	}
	snap := snaptest.MockInfo(c, yaml, nil)
	return &policy.InstallCandidate{
		Snap:            snap,
		BaseDeclaration: s.baseDecl,
	}
}

const declTempl = `type: snap-declaration
authority-id: canonical
series: 16
snap-name: @name@
snap-id: @snapid@
publisher-id: @publisher@
@plugsSlots@
timestamp: 2016-09-30T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`

func (s *baseDeclSuite) mockSnapDecl(c *C, name, snapID, publisher string, plugsSlots string) *asserts.SnapDeclaration {
	encoded := strings.Replace(declTempl, "@name@", name, 1)
	encoded = strings.Replace(encoded, "@snapid@", snapID, 1)
	encoded = strings.Replace(encoded, "@publisher@", publisher, 1)
	if plugsSlots != "" {
		encoded = strings.Replace(encoded, "@plugsSlots@", strings.TrimSpace(plugsSlots), 1)
	} else {
		encoded = strings.Replace(encoded, "@plugsSlots@\n", "", 1)
	}
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	return a.(*asserts.SnapDeclaration)
}

func (s *baseDeclSuite) TestAutoConnection(c *C) {
	all := builtin.Interfaces()

	// these have more complex or in flux policies and have their
	// own separate tests
	snowflakes := map[string]bool{
		"content":            true,
		"core-support":       true,
		"desktop":            true,
		"home":               true,
		"lxd-support":        true,
		"microstack-support": true,
		"multipass-support":  true,
		"packagekit-control": true,
		"pkcs11":             true,
		"remoteproc":         true,
		"snapd-control":      true,
		"upower-observe":     true,
		"empty":              true,
	}

	// these simply auto-connect, anything else doesn't
	autoconnect := map[string]bool{
		"audio-playback":          true,
		"browser-support":         true,
		"desktop-legacy":          true,
		"gsettings":               true,
		"media-hub":               true,
		"mir":                     true,
		"network":                 true,
		"network-bind":            true,
		"network-status":          true,
		"online-accounts-service": true,
		"opengl":                  true,
		"optical-drive":           true,
		"ros-opt-data":            true,
		"screen-inhibit-control":  true,
		"ubuntu-download-manager": true,
		"unity7":                  true,
		"unity8":                  true,
		"wayland":                 true,
		"x11":                     true,
	}

	for _, iface := range all {
		if snowflakes[iface.Name()] {
			continue
		}
		expected := autoconnect[iface.Name()]
		comm := Commentf(iface.Name())

		// check base declaration
		cand := s.connectCand(c, iface.Name(), "", "")
		arity, err := cand.CheckAutoConnect()
		if expected {
			c.Check(err, IsNil, comm)
			c.Check(arity.SlotsPerPlugAny(), Equals, false)
		} else {
			c.Check(err, NotNil, comm)
		}
	}
}

func (s *baseDeclSuite) TestAutoConnectionImplicitSlotOnly(c *C) {
	all := builtin.Interfaces()

	// these auto-connect only with an implicit slot
	autoconnect := map[string]bool{
		"desktop":        true,
		"upower-observe": true,
	}

	for _, iface := range all {
		if !autoconnect[iface.Name()] {
			continue
		}
		comm := Commentf(iface.Name())

		// check base declaration
		cand := s.connectCand(c, iface.Name(), fmt.Sprintf(`name: snapd
type: snapd
version: 0
slots:
  %s:
`, iface.Name()), "")
		arity, err := cand.CheckAutoConnect()
		c.Check(err, IsNil, comm)
		c.Check(arity.SlotsPerPlugAny(), Equals, false)
	}
}

func (s *baseDeclSuite) TestAutoConnectPlugSlot(c *C) {
	all := builtin.Interfaces()

	// these have more complex or in flux policies and have their
	// own separate tests
	snowflakes := map[string]bool{
		"classic-support": true,
		"content":         true,
		"cups-control":    true,
		"home":            true,
		"lxd-support":     true,
		// netlink-driver needs the family-name attributes to match
		"netlink-driver": true,
	}

	for _, iface := range all {
		if snowflakes[iface.Name()] {
			continue
		}
		c.Check(iface.AutoConnect(nil, nil), Equals, true)
	}
}

func (s *baseDeclSuite) TestInterimAutoConnectionHome(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()
	cand := s.connectCand(c, "home", "", "")
	arity, err := cand.CheckAutoConnect()
	c.Check(err, IsNil)
	c.Check(arity.SlotsPerPlugAny(), Equals, false)

	release.OnClassic = false
	_, err = cand.CheckAutoConnect()
	c.Check(err, ErrorMatches, `auto-connection denied by slot rule of interface \"home\"`)
}

func (s *baseDeclSuite) TestHomeReadAll(c *C) {
	const plugYaml = `name: plug-snap
version: 0
plugs:
  home:
    read: all
`
	restore := release.MockOnClassic(true)
	defer restore()
	cand := s.connectCand(c, "home", "", plugYaml)
	err := cand.Check()
	c.Check(err, NotNil)

	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)

	release.OnClassic = false
	err = cand.Check()
	c.Check(err, NotNil)

	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestHomeReadDefault(c *C) {
	const plugYaml = `name: plug-snap
version: 0
plugs:
  home: null
`
	restore := release.MockOnClassic(true)
	defer restore()
	cand := s.connectCand(c, "home", "", plugYaml)
	err := cand.Check()
	c.Check(err, IsNil)

	// Same as TestInterimAutoConnectionHome()
	arity, err := cand.CheckAutoConnect()
	c.Check(err, IsNil)
	c.Check(arity.SlotsPerPlugAny(), Equals, false)

	release.OnClassic = false
	err = cand.Check()
	c.Check(err, IsNil)

	// Same as TestInterimAutoConnectionHome()
	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestAutoConnectionSnapdControl(c *C) {
	cand := s.connectCand(c, "snapd-control", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"snapd-control\"")

	plugsSlots := `
plugs:
  snapd-control:
    allow-auto-connection: true
`

	lxdDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = lxdDecl
	arity, err := cand.CheckAutoConnect()
	c.Check(err, IsNil)
	c.Check(arity.SlotsPerPlugAny(), Equals, false)
}

func (s *baseDeclSuite) TestAutoConnectionContent(c *C) {
	// random snaps cannot connect with content
	// (Sanitize* will now also block this)
	cand := s.connectCand(c, "content", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)

	slotDecl1 := s.mockSnapDecl(c, "slot-snap", "slot-snap-id", "pub1", "")
	plugDecl1 := s.mockSnapDecl(c, "plug-snap", "plug-snap-id", "pub1", "")
	plugDecl2 := s.mockSnapDecl(c, "plug-snap", "plug-snap-id", "pub2", "")

	// same publisher, same content
	cand = s.connectCand(c, "stuff", `
name: slot-snap
version: 0
slots:
  stuff:
    interface: content
    content: mk1
`, `
name: plug-snap
version: 0
plugs:
  stuff:
    interface: content
    content: mk1
`)
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl1
	arity, err := cand.CheckAutoConnect()
	c.Check(err, IsNil)
	c.Check(arity.SlotsPerPlugAny(), Equals, false)

	// different publisher, same content
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl2
	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)

	// same publisher, different content
	cand = s.connectCand(c, "stuff", `name: slot-snap
version: 0
slots:
  stuff:
    interface: content
    content: mk1
`, `
name: plug-snap
version: 0
plugs:
  stuff:
    interface: content
    content: mk2
`)
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl1
	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestAutoConnectionSharedMemory(c *C) {
	// random snaps cannot connect with shared-memory
	// (Sanitize* will now also block this)
	cand := s.connectCand(c, "shared-memory", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)

	slotDecl1 := s.mockSnapDecl(c, "slot-snap", "slot-snap-id", "pub1", "")
	plugDecl1 := s.mockSnapDecl(c, "plug-snap", "plug-snap-id", "pub1", "")
	plugDecl2 := s.mockSnapDecl(c, "plug-snap", "plug-snap-id", "pub2", "")

	// same publisher, same shared-memory
	cand = s.connectCand(c, "stuff", `
name: slot-snap
version: 0
slots:
  stuff:
    interface: shared-memory
    shared-memory: mk1
`, `
name: plug-snap
version: 0
plugs:
  stuff:
    interface: shared-memory
    private: false
    shared-memory: mk1
`)
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl1
	arity, err := cand.CheckAutoConnect()
	c.Check(err, IsNil)
	c.Check(arity.SlotsPerPlugAny(), Equals, false)

	// different publisher, same shared-memory
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl2
	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)

	// same publisher, different shared-memory
	cand = s.connectCand(c, "stuff", `name: slot-snap
version: 0
slots:
  stuff:
    interface: shared-memory
    shared-memory: mk1
`, `
name: plug-snap
version: 0
plugs:
  stuff:
    interface: shared-memory
    private: false
    shared-memory: mk2
`)
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl1
	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestAutoConnectionSharedMemoryPrivate(c *C) {
	slotDecl := s.mockSnapDecl(c, "snapd", "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4", "canonical", "")
	appSlotDecl := s.mockSnapDecl(c, "slot-snap", "slot-snap-id", "pub1", "")
	plugDecl := s.mockSnapDecl(c, "plug-snap", "plug-snap-id", "pub1", "")

	// private shm plug, implicit slot
	cand := s.connectCand(c, "shared-memory", `
name: snapd
type: snapd
version: 0
slots:
  shared-memory:
`, `
name: plug-snap
version: 0
plugs:
  shared-memory:
    private: true
`)
	cand.SlotSnapDeclaration = slotDecl
	cand.PlugSnapDeclaration = plugDecl
	arity, err := cand.CheckAutoConnect()
	c.Check(err, IsNil)
	c.Check(arity.SlotsPerPlugAny(), Equals, false)

	// private shm plug, regular app slot
	cand = s.connectCand(c, "shared-memory", `
name: slot-snap
version: 0
slots:
  shared-memory:
`, `
name: plug-snap
version: 0
plugs:
  shared-memory:
    private: true
`)
	cand.SlotSnapDeclaration = appSlotDecl
	cand.PlugSnapDeclaration = plugDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)

	// regular shm plug, implicit slot
	cand = s.connectCand(c, "shared-memory", `
name: snapd
type: snapd
version: 0
slots:
  shared-memory:
`, `
name: plug-snap
version: 0
plugs:
  shared-memory:
    private: false
`)
	cand.SlotSnapDeclaration = slotDecl
	cand.PlugSnapDeclaration = plugDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestAutoConnectionLxdSupportOverride(c *C) {
	// by default, don't auto-connect
	cand := s.connectCand(c, "lxd-support", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)

	plugsSlots := `
plugs:
  lxd-support:
    allow-auto-connection: true
`

	lxdDecl := s.mockSnapDecl(c, "lxd", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = lxdDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionLxdSupportOverrideRevoke(c *C) {
	cand := s.connectCand(c, "lxd-support", "", "")
	plugsSlots := `
plugs:
  lxd-support:
    allow-auto-connection: false
`

	lxdDecl := s.mockSnapDecl(c, "notlxd", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = lxdDecl
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection not allowed by plug rule of interface \"lxd-support\" for \"notlxd\" snap")
}

func (s *baseDeclSuite) TestAutoConnectionKernelModuleControlOverride(c *C) {
	cand := s.connectCand(c, "kernel-module-control", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"kernel-module-control\"")

	plugsSlots := `
plugs:
  kernel-module-control:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionDockerSupportOverride(c *C) {
	cand := s.connectCand(c, "docker-support", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"docker-support\"")

	plugsSlots := `
plugs:
  docker-support:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionClassicSupportOverride(c *C) {
	cand := s.connectCand(c, "classic-support", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"classic-support\"")

	plugsSlots := `
plugs:
  classic-support:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "classic", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionKubernetesSupportOverride(c *C) {
	cand := s.connectCand(c, "kubernetes-support", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"kubernetes-support\"")

	plugsSlots := `
plugs:
  kubernetes-support:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionMicroStackSupportOverride(c *C) {
	cand := s.connectCand(c, "microstack-support", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"microstack-support\"")

	plugsSlots := `
plugs:
  microstack-support:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}
func (s *baseDeclSuite) TestAutoConnectionGreengrassSupportOverride(c *C) {
	cand := s.connectCand(c, "greengrass-support", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"greengrass-support\"")

	plugsSlots := `
plugs:
  greengrass-support:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionMultipassSupportOverride(c *C) {
	cand := s.connectCand(c, "multipass-support", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"multipass-support\"")

	plugsSlots := `
plugs:
  multipass-support:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "multipass-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionBlockDevicesOverride(c *C) {
	cand := s.connectCand(c, "block-devices", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"block-devices\"")

	plugsSlots := `
plugs:
  block-devices:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionPackagekitControlOverride(c *C) {
	cand := s.connectCand(c, "packagekit-control", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"packagekit-control\"")

	plugsSlots := `
plugs:
  packagekit-control:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionPosixMQOverride(c *C) {
	cand := s.connectCand(c, "posix-mq", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection not allowed by plug rule of interface \"posix-mq\"")

	plugsSlots := `
plugs:
  posix-mq:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionSteamSupportOverride(c *C) {
	cand := s.connectCand(c, "steam-support", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"steam-support\"")

	plugsSlots := `
plugs:
  steam-support:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionOverrideMultiple(c *C) {
	plugsSlots := `
plugs:
  network-bind:
    allow-auto-connection: true
  network-control:
    allow-auto-connection: true
  kernel-module-control:
    allow-auto-connection: true
  system-observe:
    allow-auto-connection: true
  hardware-observe:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)

	all := builtin.Interfaces()
	// these are a mixture interfaces that the snap plugs
	plugged := map[string]bool{
		"network-bind":          true,
		"network-control":       true,
		"kernel-module-control": true,
		"system-observe":        true,
		"hardware-observe":      true,
	}
	for _, iface := range all {
		if !plugged[iface.Name()] {
			continue
		}

		cand := s.connectCand(c, iface.Name(), "", "")
		cand.PlugSnapDeclaration = snapDecl
		arity, err := cand.CheckAutoConnect()
		c.Check(err, IsNil)
		c.Check(arity.SlotsPerPlugAny(), Equals, false)
	}
}

// describe installation rules for slots succinctly for cross-checking,
// if an interface is not mentioned here a slot of its type can only
// be installed by a core snap (and this was taken care by
// BeforePrepareSlot),
// otherwise the entry for the interface is the list of snap types it
// can be installed by (using the declaration naming);
// ATM a nil entry means even stricter rules that would need be tested
// separately and whose implementation is in flux for now
var (
	slotInstallation = map[string][]string{
		// other
		"adb-support":               {"core"},
		"audio-playback":            {"app", "core"},
		"audio-record":              {"app", "core"},
		"autopilot-introspection":   {"core"},
		"avahi-control":             {"app", "core"},
		"avahi-observe":             {"app", "core"},
		"bluez":                     {"app", "core"},
		"bool-file":                 {"core", "gadget"},
		"browser-support":           {"core"},
		"content":                   {"app", "gadget"},
		"core-support":              {"core"},
		"cups":                      {"app"},
		"cups-control":              {"app", "core"},
		"dbus":                      {"app"},
		"docker-support":            {"core"},
		"desktop-launch":            {"core"},
		"dsp":                       {"core", "gadget"},
		"empty":                     {"app"},
		"fwupd":                     {"app", "core"},
		"gpio":                      {"core", "gadget"},
		"gpio-control":              {"core"},
		"greengrass-support":        {"core"},
		"hidraw":                    {"core", "gadget"},
		"i2c":                       {"core", "gadget"},
		"iio":                       {"core", "gadget"},
		"kernel-module-load":        {"core"},
		"kubernetes-support":        {"core"},
		"location-control":          {"app"},
		"location-observe":          {"app"},
		"lxd-support":               {"core"},
		"maliit":                    {"app"},
		"media-hub":                 {"app", "core"},
		"mir":                       {"app"},
		"microstack-support":        {"core"},
		"modem-manager":             {"app", "core"},
		"mount-control":             {"core"},
		"mpris":                     {"app"},
		"netlink-driver":            {"core", "gadget"},
		"network-manager":           {"app", "core"},
		"network-manager-observe":   {"app", "core"},
		"network-status":            {"core"},
		"ofono":                     {"app", "core"},
		"online-accounts-service":   {"app"},
		"power-control":             {"core"},
		"ppp":                       {"core"},
		"polkit-agent":              {"core"},
		"pulseaudio":                {"app", "core"},
		"pwm":                       {"core", "gadget"},
		"qualcomm-ipc-router":       {"core", "app"},
		"raw-volume":                {"core", "gadget"},
		"scsi-generic":              {"core"},
		"sd-control":                {"core"},
		"serial-port":               {"core", "gadget"},
		"spi":                       {"core", "gadget"},
		"steam-support":             {"core"},
		"storage-framework-service": {"app"},
		"thumbnailer-service":       {"app"},
		"ubuntu-download-manager":   {"app"},
		"udisks2":                   {"app", "core"},
		"uhid":                      {"core"},
		"uio":                       {"core", "gadget"},
		"unity8":                    {"app"},
		"unity8-calendar":           {"app"},
		"unity8-contacts":           {"app"},
		"upower-observe":            {"app", "core"},
		"userns":                    {"core"},
		"wayland":                   {"app", "core"},
		"x11":                       {"app", "core"},
		// snowflakes
		"classic-support": nil,
		"custom-device":   nil,
		"docker":          nil,
		"lxd":             nil,
		"microceph":       nil,
		"microovn":        nil,
		"pkcs11":          nil,
		"posix-mq":        nil,
		"shared-memory":   nil,
	}

	restrictedPlugInstallation = map[string][]string{
		"core-support": {"core"},
	}

	snapTypeMap = map[string]snap.Type{
		"core":   snap.TypeOS,
		"app":    snap.TypeApp,
		"kernel": snap.TypeKernel,
		"gadget": snap.TypeGadget,
	}
)

func (s *baseDeclSuite) TestSlotInstallation(c *C) {
	all := builtin.Interfaces()

	for _, iface := range all {
		types, ok := slotInstallation[iface.Name()]
		if !ok { // common ones, only core can install them,
			types = []string{"core"}
		}

		// only restricted slots can use the AppArmor
		// unconfined profile mode so check that this
		// slot is not using it
		c.Assert(interfaces.StaticInfoOf(iface).AppArmorUnconfinedSlots, Equals, false)
		if types == nil {
			// snowflake needs to be tested specially
			continue
		}
		for name, snapType := range snapTypeMap {
			ok := strutil.ListContains(types, name)
			ic := s.installSlotCand(c, iface.Name(), snapType, ``)
			err := ic.Check()
			comm := Commentf("%s by %s snap", iface.Name(), name)
			if ok {
				c.Check(err, IsNil, comm)
			} else {
				c.Check(err, NotNil, comm)
			}
		}
	}

	// test desktop specifically
	ic := s.installSlotCand(c, "desktop", snap.TypeApp, ``)
	err := ic.Check()
	c.Check(err, Not(IsNil))
	c.Check(err, ErrorMatches, "installation denied by \"desktop\" slot rule of interface \"desktop\"")
	// ... but the minimal check (used by --dangerous) allows installation
	icMin := &policy.InstallCandidateMinimalCheck{
		Snap:            ic.Snap,
		BaseDeclaration: s.baseDecl,
	}
	err = icMin.Check()
	c.Check(err, IsNil)

	// test docker specially
	ic = s.installSlotCand(c, "docker", snap.TypeApp, ``)
	err = ic.Check()
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "installation not allowed by \"docker\" slot rule of interface \"docker\"")

	// test lxd specially
	ic = s.installSlotCand(c, "lxd", snap.TypeApp, ``)
	err = ic.Check()
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "installation not allowed by \"lxd\" slot rule of interface \"lxd\"")

	// test microceph specially
	ic = s.installSlotCand(c, "microceph", snap.TypeApp, ``)
	err = ic.Check()
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "installation not allowed by \"microceph\" slot rule of interface \"microceph\"")

	// test microovn specially
	ic = s.installSlotCand(c, "microovn", snap.TypeApp, ``)
	err = ic.Check()
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "installation not allowed by \"microovn\" slot rule of interface \"microovn\"")

	// test shared-memory specially
	ic = s.installSlotCand(c, "shared-memory", snap.TypeApp, ``)
	err = ic.Check()
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "installation denied by \"shared-memory\" slot rule of interface \"shared-memory\"")

	// The core and snapd snaps may provide a shared-memory slot
	ic = s.installSlotCand(c, "shared-memory", snap.TypeOS, `name: core
version: 0
type: os
slots:
  shared-memory:
`)
	ic.SnapDeclaration = s.mockSnapDecl(c, "core", "99T7MUlRhtI3U0QFgl5mXXESAiSwt776", "canonical", "")
	c.Assert(ic.Check(), IsNil)

	ic = s.installSlotCand(c, "shared-memory", snap.TypeSnapd, `name: snapd
version: 0
type: snapd
slots:
  shared-memory:
`)
	ic.SnapDeclaration = s.mockSnapDecl(c, "snapd", "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4", "canonical", "")
	c.Assert(ic.Check(), IsNil)

	ic = s.installSlotCand(c, "udisks2", snap.TypeApp, `name: udisks2
version: 0
type: app
slots:
  udisks2:
`)
	err = ic.Check()
	c.Assert(err, IsNil)

	ic = s.installSlotCand(c, "udisks2", snap.TypeApp, `name: udisks2
version: 0
type: app
slots:
  udisks2:
    udev-file: some/file
`)
	err = ic.Check()
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "installation not allowed by \"udisks2\" slot rule of interface \"udisks2\"")
}

func (s *baseDeclSuite) TestPlugInstallation(c *C) {
	all := builtin.Interfaces()

	restricted := map[string]bool{
		"block-devices":           true,
		"classic-support":         true,
		"desktop-launch":          true,
		"dm-crypt":                true,
		"docker-support":          true,
		"greengrass-support":      true,
		"gpio-control":            true,
		"ion-memory-control":      true,
		"kernel-firmware-control": true,
		"kernel-module-control":   true,
		"kernel-module-load":      true,
		"kubernetes-support":      true,
		"lxd-support":             true,
		"microstack-support":      true,
		"mount-control":           true,
		"multipass-support":       true,
		"nvidia-drivers-support":  true,
		"packagekit-control":      true,
		"personal-files":          true,
		"polkit":                  true,
		"polkit-agent":            true,
		"remoteproc":              true,
		"sd-control":              true,
		"shutdown":                true,
		"snap-refresh-control":    true,
		"snap-themes-control":     true,
		"snap-refresh-observe":    true,
		"snapd-control":           true,
		"steam-support":           true,
		"system-files":            true,
		"tee":                     true,
		"uinput":                  true,
		"unity8":                  true,
		"userns":                  true,
		"xilinx-dma":              true,
	}

	for _, iface := range all {
		types, ok := restrictedPlugInstallation[iface.Name()]
		// If plug installation is restricted to specific snap types we
		// need to make sure this is really the case here. If that is not
		// the case we continue as normal.
		if ok {
			// only restricted plugs can use the AppArmor
			// unconfined profile mode so check that this
			// plug is not using it
			c.Assert(interfaces.StaticInfoOf(iface).AppArmorUnconfinedPlugs, Equals, false)
			for name, snapType := range snapTypeMap {
				ok := strutil.ListContains(types, name)
				ic := s.installPlugCand(c, iface.Name(), snapType, ``)
				err := ic.Check()
				comm := Commentf("%s by %s snap", iface.Name(), name)
				if ok {
					c.Check(err, IsNil, comm)
				} else {
					c.Check(err, NotNil, comm)
				}
			}
		} else {
			ic := s.installPlugCand(c, iface.Name(), snap.TypeApp, ``)
			err := ic.Check()
			comm := Commentf("%s", iface.Name())
			if restricted[iface.Name()] {
				c.Check(err, NotNil, comm)
			} else {
				c.Check(err, IsNil, comm)
				// only restricted plugs can use the AppArmor
				// unconfined profile mode so check that this
				// plug is not using it
				c.Assert(interfaces.StaticInfoOf(iface).AppArmorUnconfinedPlugs, Equals, false)
			}
		}
	}
}

func (s *baseDeclSuite) TestConnection(c *C) {
	all := builtin.Interfaces()

	// connecting with these interfaces needs to be allowed on
	// case-by-case basis
	noconnect := map[string]bool{
		"content":                   true,
		"cups":                      true,
		"custom-device":             true,
		"desktop":                   true,
		"docker":                    true,
		"fwupd":                     true,
		"location-control":          true,
		"location-observe":          true,
		"lxd":                       true,
		"maliit":                    true,
		"microceph":                 true,
		"microovn":                  true,
		"mir":                       true,
		"online-accounts-service":   true,
		"posix-mq":                  true,
		"qualcomm-ipc-router":       true,
		"raw-volume":                true,
		"shared-memory":             true,
		"storage-framework-service": true,
		"thumbnailer-service":       true,
		"ubuntu-download-manager":   true,
		"unity8-calendar":           true,
		"unity8-contacts":           true,
		"upower-observe":            true,
	}

	for _, iface := range all {
		expected := !noconnect[iface.Name()]
		comm := Commentf(iface.Name())

		// check base declaration
		cand := s.connectCand(c, iface.Name(), "", "")
		err := cand.Check()

		if expected {
			c.Check(err, IsNil, comm)
		} else {
			c.Check(err, NotNil, comm)
		}
	}
}

func (s *baseDeclSuite) TestConnectionImplicitSlotOnly(c *C) {
	all := builtin.Interfaces()

	// these allow connect only with an implicit slot
	autoconnect := map[string]bool{
		"desktop":             true,
		"qualcomm-ipc-router": true,
		"upower-observe":      true,
	}

	for _, iface := range all {
		if !autoconnect[iface.Name()] {
			continue
		}
		comm := Commentf(iface.Name())

		// check base declaration
		cand := s.connectCand(c, iface.Name(), fmt.Sprintf(`name: snapd
type: snapd
version: 0
slots:
  %s:
`, iface.Name()), "")
		err := cand.Check()
		c.Check(err, IsNil, comm)
	}
}

func (s *baseDeclSuite) TestConnectionOnClassic(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	all := builtin.Interfaces()

	// connecting with these interfaces needs to be allowed on
	// case-by-case basis when not on classic
	noconnect := map[string]bool{
		"audio-record":    true,
		"modem-manager":   true,
		"network-manager": true,
		"ofono":           true,
		"pulseaudio":      true,
	}

	for _, onClassic := range []bool{true, false} {
		release.OnClassic = onClassic
		for _, iface := range all {
			if !noconnect[iface.Name()] {
				continue
			}
			expected := onClassic
			comm := Commentf(iface.Name())

			// check base declaration
			cand := s.connectCand(c, iface.Name(), "", "")
			err := cand.Check()

			if expected {
				c.Check(err, IsNil, comm)
			} else {
				c.Check(err, NotNil, comm)
			}
		}
	}
}

func (s *baseDeclSuite) TestConnectionImplicitOnClassicOrAppSnap(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	all := builtin.Interfaces()

	// These interfaces represent when the interface might be implicit on
	// classic or when the interface is provided via an app snap. As such,
	// they all share the following:
	//
	// - implicitOnCore: false
	// - implicitOnClassis: true
	// - base declaration uses:
	//     allow-installation:
	//       slot-snap-type:
	//         - app
	//         - core
	//     deny-connection:
	//       on-classic: false
	//     deny-auto-connection: true|false|unspecified
	//
	// connecting with these interfaces needs to be allowed on
	// case-by-case basis when not on classic
	ifaces := map[string]bool{
		"audio-playback":  true,
		"audio-record":    true,
		"cups-control":    true,
		"modem-manager":   true,
		"network-manager": true,
		"ofono":           true,
		"pulseaudio":      true,
	}

	for _, iface := range all {
		if !ifaces[iface.Name()] {
			continue
		}
		comm := Commentf(iface.Name())

		// verify the interface is setup as expected wrt
		// implicitOnCore, implicitOnClassic, no plugs and has
		// expected slots (ignoring AutoConnection)
		si := interfaces.StaticInfoOf(iface)
		c.Assert(si.ImplicitOnCore, Equals, false)
		c.Assert(si.ImplicitOnClassic, Equals, true)

		c.Assert(s.baseDecl.PlugRule(iface.Name()), IsNil)

		sr := s.baseDecl.SlotRule(iface.Name())
		c.Assert(sr, Not(IsNil))
		c.Assert(sr.AllowInstallation, HasLen, 1)
		c.Check(sr.AllowInstallation[0].SlotSnapTypes, DeepEquals, []string{"app", "core"}, comm)
		c.Assert(sr.DenyConnection, HasLen, 1)
		c.Check(sr.DenyConnection[0].OnClassic, DeepEquals, &asserts.OnClassicConstraint{Classic: false}, comm)

		for _, onClassic := range []bool{true, false} {
			release.OnClassic = onClassic

			for _, implicit := range []bool{true, false} {
				// When implicitOnCore is false, there is
				// nothing to test on Core
				if implicit && !onClassic {
					continue
				}

				snapType := "app"
				if implicit {
					snapType = "os"
				}
				slotYaml := fmt.Sprintf(`name: slot-snap
version: 0
type: %s
slots:
  %s:
`, snapType, iface.Name())

				// XXX: eventually 'onClassic && !implicit' but
				// the current declaration allows connection on
				// Core when 'type: app'. See:
				// https://github.com/snapcore/snapd/pull/8920/files#r471678529
				expected := onClassic

				// check base declaration
				cand := s.connectCand(c, iface.Name(), slotYaml, "")
				err := cand.Check()

				if expected {
					c.Check(err, IsNil, comm)
				} else {
					c.Check(err, NotNil, comm)
				}
			}
		}
	}
}

func (s *baseDeclSuite) TestValidity(c *C) {
	all := builtin.Interfaces()

	// these interfaces have rules both for the slots and plugs side
	// given how the rules work this can be delicate,
	// listed here to make sure that was a conscious decision
	bothSides := map[string]bool{
		"block-devices":           true,
		"audio-playback":          true,
		"classic-support":         true,
		"core-support":            true,
		"custom-device":           true,
		"desktop-launch":          true,
		"dm-crypt":                true,
		"docker-support":          true,
		"greengrass-support":      true,
		"gpio-control":            true,
		"ion-memory-control":      true,
		"kernel-firmware-control": true,
		"kernel-module-control":   true,
		"kernel-module-load":      true,
		"kubernetes-support":      true,
		"lxd-support":             true,
		"microstack-support":      true,
		"mount-control":           true,
		"multipass-support":       true,
		"nvidia-drivers-support":  true,
		"packagekit-control":      true,
		"personal-files":          true,
		"pkcs11":                  true,
		"posix-mq":                true,
		"polkit":                  true,
		"polkit-agent":            true,
		"remoteproc":              true,
		"qualcomm-ipc-router":     true,
		"sd-control":              true,
		"shutdown":                true,
		"shared-memory":           true,
		"snap-refresh-control":    true,
		"snap-themes-control":     true,
		"snap-refresh-observe":    true,
		"snapd-control":           true,
		"steam-support":           true,
		"system-files":            true,
		"tee":                     true,
		"udisks2":                 true,
		"uinput":                  true,
		"unity8":                  true,
		"userns":                  true,
		"wayland":                 true,
		"xilinx-dma":              true,
	}

	for _, iface := range all {
		plugRule := s.baseDecl.PlugRule(iface.Name())
		slotRule := s.baseDecl.SlotRule(iface.Name())
		if plugRule == nil && slotRule == nil {
			c.Logf("%s is not considered in the base declaration", iface.Name())
			c.Fail()
		}
		if plugRule != nil && slotRule != nil {
			if !bothSides[iface.Name()] {
				c.Logf("%s have both a base declaration slot rule and plug rule, make sure that's intended and correct", iface.Name())
				c.Fail()
			}
		}
	}
}

func (s *baseDeclSuite) TestConnectionContent(c *C) {
	// we let connect explicitly as long as content matches (or is absent on both sides)

	// random (Sanitize* will now also block this)
	cand := s.connectCand(c, "content", "", "")
	err := cand.Check()
	c.Check(err, NotNil)

	slotDecl1 := s.mockSnapDecl(c, "slot-snap", "slot-snap-id", "pub1", "")
	plugDecl1 := s.mockSnapDecl(c, "plug-snap", "plug-snap-id", "pub1", "")
	plugDecl2 := s.mockSnapDecl(c, "plug-snap", "plug-snap-id", "pub2", "")

	// same publisher, same content
	cand = s.connectCand(c, "stuff", `name: slot-snap
version: 0
slots:
  stuff:
    interface: content
    content: mk1
`, `
name: plug-snap
version: 0
plugs:
  stuff:
    interface: content
    content: mk1
`)
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl1
	err = cand.Check()
	c.Check(err, IsNil)

	// different publisher, same content
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl2
	err = cand.Check()
	c.Check(err, IsNil)

	// same publisher, different content
	cand = s.connectCand(c, "stuff", `
name: slot-snap
version: 0
slots:
  stuff:
    interface: content
    content: mk1
`, `
name: plug-snap
version: 0
plugs:
  stuff:
    interface: content
    content: mk2
`)
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl1
	err = cand.Check()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestConnectionQualcommIpcRouter(c *C) {
	// we let connect explicitly as long as qcipc matches

	slotDecl1 := s.mockSnapDecl(c, "slot-snap", "slot-snap-id", "pub1", "")
	plugDecl1 := s.mockSnapDecl(c, "plug-snap", "plug-snap-id", "pub1", "")

	// Same qcipc label
	cand := s.connectCand(c, "qc-router", `name: slot-snap
version: 0
slots:
  qc-router:
    interface: qualcomm-ipc-router
    qcipc: monitor
    address: abcd
`, `
name: plug-snap
version: 0
plugs:
  qc-router:
    interface: qualcomm-ipc-router
    qcipc: monitor
`)
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl1
	err := cand.Check()
	c.Check(err, IsNil)

	// Different qcipc label
	cand = s.connectCand(c, "qc-router", `name: slot-snap
version: 0
slots:
  qc-router:
    interface: qualcomm-ipc-router
    qcipc: monitor
    address: abcd
`, `
name: plug-snap
version: 0
plugs:
  qc-router:
    interface: qualcomm-ipc-router
    qcipc: other
`)
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl1
	err = cand.Check()
	c.Check(err.Error(), Equals, `connection not allowed by slot rule of interface "qualcomm-ipc-router"`)

	// Legacy case with slot provided by system
	cand = s.connectCand(c, "qualcomm-ipc-router", `name: snapd
version: 0
type: snapd
slots:
  qualcomm-ipc-router:
`, `
name: plug-snap
version: 0
plugs:
  qualcomm-ipc-router:
`)
	cand.SlotSnapDeclaration = s.mockSnapDecl(c, "snapd", "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4", "canonical", "")
	cand.PlugSnapDeclaration = plugDecl1
	err = cand.Check()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestConnectionSharedMemory(c *C) {
	// we let connect explicitly as long as shared-memory matches

	// random (Sanitize* will now also block this)
	cand := s.connectCand(c, "shared-memory", "", "")
	err := cand.Check()
	c.Check(err, NotNil)

	slotDecl1 := s.mockSnapDecl(c, "slot-snap", "slot-snap-id", "pub1", "")
	plugDecl1 := s.mockSnapDecl(c, "plug-snap", "plug-snap-id", "pub1", "")
	plugDecl2 := s.mockSnapDecl(c, "plug-snap", "plug-snap-id", "pub2", "")

	// same publisher, same shared-memory
	cand = s.connectCand(c, "stuff", `name: slot-snap
version: 0
slots:
  stuff:
    interface: shared-memory
    shared-memory: mk1
`, `
name: plug-snap
version: 0
plugs:
  stuff:
    interface: shared-memory
    private: false
    shared-memory: mk1
`)
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl1
	err = cand.Check()
	c.Check(err, IsNil)

	// different publisher, same shared-memory
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl2
	err = cand.Check()
	c.Check(err, IsNil)

	// same publisher, different shared-memory
	cand = s.connectCand(c, "stuff", `
name: slot-snap
version: 0
slots:
  stuff:
    interface: shared-memory
    shared-memory: mk1
`, `
name: plug-snap
version: 0
plugs:
  stuff:
    interface: shared-memory
    private: false
    shared-memory: mk2
`)
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl1
	err = cand.Check()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestConnectionSharedMemoryPrivate(c *C) {
	slotDecl := s.mockSnapDecl(c, "snapd", "PMrrV4ml8uWuEUDBT8dSGnKUYbevVhc4", "canonical", "")
	appSlotDecl := s.mockSnapDecl(c, "slot-snap", "slot-snap-id", "pub1", "")
	plugDecl := s.mockSnapDecl(c, "plug-snap", "plug-snap-id", "pub1", "")

	// private shm plug, implicit slot
	cand := s.connectCand(c, "shared-memory", `name: snapd
type: snapd
version: 0
slots:
  shared-memory:
`, `
name: plug-snap
version: 0
plugs:
  shared-memory:
    private: true
`)
	cand.SlotSnapDeclaration = slotDecl
	cand.PlugSnapDeclaration = plugDecl
	err := cand.Check()
	c.Check(err, IsNil)

	// private shm plug, regular app slot
	cand = s.connectCand(c, "shared-memory", `name: slot-snap
version: 0
slots:
  shared-memory:
    shared-memory: mk1
`, `
name: plug-snap
version: 0
plugs:
  shared-memory:
    private: true
`)
	cand.SlotSnapDeclaration = appSlotDecl
	cand.PlugSnapDeclaration = plugDecl
	err = cand.Check()
	c.Check(err, NotNil)

	// regular shm plug, implicit slot
	cand = s.connectCand(c, "shared-memory", `name: snapd
type: snapd
version: 0
slots:
  shared-memory:
`, `
name: plug-snap
version: 0
plugs:
  shared-memory:
    shared-memory: mk1
    private: false
`)
	cand.SlotSnapDeclaration = slotDecl
	cand.PlugSnapDeclaration = plugDecl
	err = cand.Check()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestComposeBaseDeclaration(c *C) {
	decl, err := policy.ComposeBaseDeclaration(nil)
	c.Assert(err, IsNil)
	c.Assert(string(decl), testutil.Contains, `
type: base-declaration
authority-id: canonical
series: 16
revision: 0
`)
}

func (s *baseDeclSuite) TestDoesNotPanic(c *C) {
	// In case there are any issues in the actual interfaces we'd get a panic
	// on snapd startup. This test prevents this from happing unnoticed.
	_, err := policy.ComposeBaseDeclaration(builtin.Interfaces())
	c.Assert(err, IsNil)
}

func (s *baseDeclSuite) TestBrowserSupportAllowSandbox(c *C) {
	const plugYaml = `name: plug-snap
version: 0
plugs:
  browser-support:
   allow-sandbox: true
`
	cand := s.connectCand(c, "browser-support", "", plugYaml)
	err := cand.Check()
	c.Check(err, NotNil)

	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestOpticalDriveWrite(c *C) {
	type options struct {
		readonlyYamls []string
		writableYamls []string
	}

	opts := &options{
		readonlyYamls: []string{
			// Non-specified "write" attribute
			`name: plug-snap
version: 0
plugs:
  optical-drive: null
`,
			// Undefined "write" attribute
			`name: plug-snap
version: 0
plugs:
  optical-drive: {}
`,
			// False "write" attribute
			`name: plug-snap
version: 0
plugs:
  optical-drive:
    write: false
`,
		},
		writableYamls: []string{
			// True "write" attribute
			`name: plug-snap
version: 0
plugs:
  optical-drive:
    write: true
`,
		},
	}

	checkOpticalDriveAutoConnect := func(plugYaml string, checker Checker) {
		cand := s.connectCand(c, "optical-drive", "", plugYaml)
		err := cand.Check()
		c.Check(err, checker)
		_, err = cand.CheckAutoConnect()
		c.Check(err, checker)
	}

	for _, plugYaml := range opts.readonlyYamls {
		checkOpticalDriveAutoConnect(plugYaml, IsNil)
	}
	for _, plugYaml := range opts.writableYamls {
		checkOpticalDriveAutoConnect(plugYaml, NotNil)
	}
}

func (s *baseDeclSuite) TestRawVolumeOverride(c *C) {
	slotYaml := `name: slot-snap
type: gadget
version: 0
slots:
  raw-volume:
    path: /dev/mmcblk0p1
`
	slotSnap := snaptest.MockInfo(c, slotYaml, nil)
	// mock a well-formed slot snap decl with SnapID
	slotSnapDecl := s.mockSnapDecl(c, "slot-snap", "slotsnapidididididididididididid", "canonical", "")

	plugYaml := `name: plug-snap
version: 0
plugs:
  raw-volume:
`
	plugSnap := snaptest.MockInfo(c, plugYaml, nil)

	// no plug-side declaration
	cand := &policy.ConnectCandidate{
		Plug:                interfaces.NewConnectedPlug(plugSnap.Plugs["raw-volume"], nil, nil),
		Slot:                interfaces.NewConnectedSlot(slotSnap.Slots["raw-volume"], nil, nil),
		SlotSnapDeclaration: slotSnapDecl,
		BaseDeclaration:     s.baseDecl,
	}

	err := cand.Check()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "connection denied by slot rule of interface \"raw-volume\"")
	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by slot rule of interface \"raw-volume\"")

	// specific plug-side declaration for connection only
	plugsOverride := `
plugs:
  raw-volume:
    allow-connection:
      slot-snap-id:
        - slotsnapidididididididididididid
    allow-auto-connection: false
`
	plugSnapDecl := s.mockSnapDecl(c, "plug-snap", "plugsnapidididididididididididid", "canonical", plugsOverride)
	cand.PlugSnapDeclaration = plugSnapDecl
	err = cand.Check()
	c.Check(err, IsNil)
	_, err = cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection not allowed by plug rule of interface \"raw-volume\" for \"plug-snap\" snap")

	// specific plug-side declaration for connection and auto-connection
	plugsOverride = `
plugs:
  raw-volume:
    allow-connection:
      slot-snap-id:
        - slotsnapidididididididididididid
    allow-auto-connection:
      slot-snap-id:
        - slotsnapidididididididididididid
`
	plugSnapDecl = s.mockSnapDecl(c, "plug-snap", "plugsnapidididididididididididid", "canonical", plugsOverride)
	cand.PlugSnapDeclaration = plugSnapDecl
	err = cand.Check()
	c.Check(err, IsNil)
	arity, err := cand.CheckAutoConnect()
	c.Check(err, IsNil)
	c.Check(arity.SlotsPerPlugAny(), Equals, false)

	// blanket allow for connection and auto-connection to any slotting snap
	plugsOverride = `
plugs:
  raw-volume:
    allow-connection: true
    allow-auto-connection: true
`
	plugSnapDecl = s.mockSnapDecl(c, "some-snap", "plugsnapidididididididididididid", "canonical", plugsOverride)
	cand.PlugSnapDeclaration = plugSnapDecl
	err = cand.Check()
	c.Check(err, IsNil)
	arity, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
	c.Check(arity.SlotsPerPlugAny(), Equals, false)
}

func (s *baseDeclSuite) TestAutoConnectionDesktopLaunchOverride(c *C) {
	cand := s.connectCand(c, "desktop-launch", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"desktop-launch\"")

	plugsSlots := `
plugs:
  desktop-launch:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "some-snap-with-desktop-launch-id", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionPolkitAgentOverride(c *C) {
	cand := s.connectCand(c, "polkit-agent", "", "")
	_, err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"polkit-agent\"")

	plugsSlots := `
plugs:
  polkit-agent:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "some-snap-with-polkit-agent-id", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	_, err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}
