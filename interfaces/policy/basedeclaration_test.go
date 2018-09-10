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
		"content":       true,
		"core-support":  true,
		"home":          true,
		"lxd-support":   true,
		"snapd-control": true,
		"dummy":         true,
	}

	// these simply auto-connect, anything else doesn't
	autoconnect := map[string]bool{
		"browser-support":         true,
		"desktop":                 true,
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
		"pulseaudio":              true,
		"screen-inhibit-control":  true,
		"ubuntu-download-manager": true,
		"unity7":                  true,
		"unity8":                  true,
		"upower-observe":          true,
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
		err := cand.CheckAutoConnect()
		if expected {
			c.Check(err, IsNil, comm)
		} else {
			c.Check(err, NotNil, comm)
		}
	}
}

func (s *baseDeclSuite) TestAutoConnectPlugSlot(c *C) {
	all := builtin.Interfaces()

	// these have more complex or in flux policies and have their
	// own separate tests
	snowflakes := map[string]bool{
		"classic-support": true,
		"content":         true,
		"home":            true,
		"lxd-support":     true,
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
	err := cand.CheckAutoConnect()
	c.Check(err, IsNil)

	release.OnClassic = false
	err = cand.CheckAutoConnect()
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

	err = cand.CheckAutoConnect()
	c.Check(err, NotNil)

	release.OnClassic = false
	err = cand.Check()
	c.Check(err, NotNil)

	err = cand.CheckAutoConnect()
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
	err = cand.CheckAutoConnect()
	c.Check(err, IsNil)

	release.OnClassic = false
	err = cand.Check()
	c.Check(err, IsNil)

	// Same as TestInterimAutoConnectionHome()
	err = cand.CheckAutoConnect()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestAutoConnectionSnapdControl(c *C) {
	cand := s.connectCand(c, "snapd-control", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"snapd-control\"")

	plugsSlots := `
plugs:
  snapd-control:
    allow-auto-connection: true
`

	lxdDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = lxdDecl
	err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionContent(c *C) {
	// random snaps cannot connect with content
	// (Sanitize* will now also block this)
	cand := s.connectCand(c, "content", "", "")
	err := cand.CheckAutoConnect()
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
	err = cand.CheckAutoConnect()
	c.Check(err, IsNil)

	// different publisher, same content
	cand.SlotSnapDeclaration = slotDecl1
	cand.PlugSnapDeclaration = plugDecl2
	err = cand.CheckAutoConnect()
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
	err = cand.CheckAutoConnect()
	c.Check(err, NotNil)
}

func (s *baseDeclSuite) TestAutoConnectionLxdSupportOverride(c *C) {
	// by default, don't auto-connect
	cand := s.connectCand(c, "lxd-support", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, NotNil)

	plugsSlots := `
plugs:
  lxd-support:
    allow-auto-connection: true
`

	lxdDecl := s.mockSnapDecl(c, "lxd", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = lxdDecl
	err = cand.CheckAutoConnect()
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
	err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection not allowed by plug rule of interface \"lxd-support\" for \"notlxd\" snap")
}

func (s *baseDeclSuite) TestAutoConnectionKernelModuleControlOverride(c *C) {
	cand := s.connectCand(c, "kernel-module-control", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"kernel-module-control\"")

	plugsSlots := `
plugs:
  kernel-module-control:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionDockerSupportOverride(c *C) {
	cand := s.connectCand(c, "docker-support", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"docker-support\"")

	plugsSlots := `
plugs:
  docker-support:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionClassicSupportOverride(c *C) {
	cand := s.connectCand(c, "classic-support", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"classic-support\"")

	plugsSlots := `
plugs:
  classic-support:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "classic", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionKubernetesSupportOverride(c *C) {
	cand := s.connectCand(c, "kubernetes-support", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"kubernetes-support\"")

	plugsSlots := `
plugs:
  kubernetes-support:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	err = cand.CheckAutoConnect()
	c.Check(err, IsNil)
}

func (s *baseDeclSuite) TestAutoConnectionGreengrassSupportOverride(c *C) {
	cand := s.connectCand(c, "greengrass-support", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, NotNil)
	c.Assert(err, ErrorMatches, "auto-connection denied by plug rule of interface \"greengrass-support\"")

	plugsSlots := `
plugs:
  greengrass-support:
    allow-auto-connection: true
`

	snapDecl := s.mockSnapDecl(c, "some-snap", "J60k4JY0HppjwOjW8dZdYc8obXKxujRu", "canonical", plugsSlots)
	cand.PlugSnapDeclaration = snapDecl
	err = cand.CheckAutoConnect()
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
		err := cand.CheckAutoConnect()
		c.Check(err, IsNil)
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
	unconstrained = []string{"core", "kernel", "gadget", "app"}

	slotInstallation = map[string][]string{
		// other
		"autopilot-introspection": {"core"},
		"avahi-control":           {"app", "core"},
		"avahi-observe":           {"app", "core"},
		"bluez":                   {"app", "core"},
		"bool-file":               {"core", "gadget"},
		"browser-support":         {"core"},
		"content":                 {"app", "gadget"},
		"core-support":            {"core"},
		"dbus":                    {"app"},
		"docker-support":          {"core"},
		"fwupd":                   {"app"},
		"gpio":                    {"core", "gadget"},
		"greengrass-support":      {"core"},
		"hidraw":                  {"core", "gadget"},
		"i2c":                     {"core", "gadget"},
		"iio":                     {"core", "gadget"},
		"kubernetes-support":      {"core"},
		"location-control":        {"app"},
		"location-observe":        {"app"},
		"lxd-support":             {"core"},
		"maliit":                  {"app"},
		"media-hub":               {"app", "core"},
		"mir":                     {"app"},
		"modem-manager":           {"app", "core"},
		"mpris":                   {"app"},
		"network-manager":         {"app", "core"},
		"network-status":          {"app"},
		"ofono":                   {"app", "core"},
		"online-accounts-service": {"app"},
		"ppp":         {"core"},
		"pulseaudio":  {"app", "core"},
		"serial-port": {"core", "gadget"},
		"spi":         {"core", "gadget"},
		"storage-framework-service": {"app"},
		"dummy":                     {"app"},
		"thumbnailer-service":       {"app"},
		"ubuntu-download-manager":   {"app"},
		"udisks2":                   {"app", "core"},
		"uhid":                      {"core"},
		"unity8":                    {"app"},
		"unity8-calendar":           {"app"},
		"unity8-contacts":           {"app"},
		"upower-observe":            {"app", "core"},
		"wayland":                   {"app", "core"},
		"x11":                       {"app", "core"},
		// snowflakes
		"classic-support": nil,
		"docker":          nil,
		"lxd":             nil,
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
		compareWithSanitize := false
		if !ok { // common ones, only core can install them,
			// their plain BeforePrepareSlot checked for that
			types = []string{"core"}
			compareWithSanitize = true
		}
		if types == nil {
			// snowflake needs to be tested specially
			continue
		}
		for name, snapType := range snapTypeMap {
			ok := strutil.ListContains(types, name)
			ic := s.installSlotCand(c, iface.Name(), snapType, ``)
			slotInfo := ic.Snap.Slots[iface.Name()]
			err := ic.Check()
			comm := Commentf("%s by %s snap", iface.Name(), name)
			if ok {
				c.Check(err, IsNil, comm)
			} else {
				c.Check(err, NotNil, comm)
			}
			if compareWithSanitize {
				sanitizeErr := interfaces.BeforePrepareSlot(iface, slotInfo)
				if err == nil {
					c.Check(sanitizeErr, IsNil, comm)
				} else {
					c.Check(sanitizeErr, NotNil, comm)
				}
			}
		}
	}

	// test docker specially
	ic := s.installSlotCand(c, "docker", snap.TypeApp, ``)
	err := ic.Check()
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "installation not allowed by \"docker\" slot rule of interface \"docker\"")

	// test lxd specially
	ic = s.installSlotCand(c, "lxd", snap.TypeApp, ``)
	err = ic.Check()
	c.Assert(err, Not(IsNil))
	c.Assert(err, ErrorMatches, "installation not allowed by \"lxd\" slot rule of interface \"lxd\"")
}

func (s *baseDeclSuite) TestPlugInstallation(c *C) {
	all := builtin.Interfaces()

	restricted := map[string]bool{
		"classic-support":       true,
		"docker-support":        true,
		"greengrass-support":    true,
		"kernel-module-control": true,
		"kubernetes-support":    true,
		"lxd-support":           true,
		"snapd-control":         true,
		"unity8":                true,
	}

	for _, iface := range all {
		types, ok := restrictedPlugInstallation[iface.Name()]
		// If plug installation is restricted to specific snap types we
		// need to make sure this is really the case here. If that is not
		// the case we continue as normal.
		if ok {
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
		"docker":                    true,
		"fwupd":                     true,
		"location-control":          true,
		"location-observe":          true,
		"lxd":                       true,
		"maliit":                    true,
		"mir":                       true,
		"network-status":            true,
		"online-accounts-service":   true,
		"storage-framework-service": true,
		"thumbnailer-service":       true,
		"ubuntu-download-manager":   true,
		"unity8-calendar":           true,
		"unity8-contacts":           true,
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

func (s *baseDeclSuite) TestConnectionOnClassic(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	all := builtin.Interfaces()

	// connecting with these interfaces needs to be allowed on
	// case-by-case basis when not on classic
	noconnect := map[string]bool{
		"modem-manager":   true,
		"network-manager": true,
		"ofono":           true,
		"pulseaudio":      true,
		"upower-observe":  true,
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

func (s *baseDeclSuite) TestSanity(c *C) {
	all := builtin.Interfaces()

	// these interfaces have rules both for the slots and plugs side
	// given how the rules work this can be delicate,
	// listed here to make sure that was a conscious decision
	bothSides := map[string]bool{
		"classic-support":       true,
		"core-support":          true,
		"docker-support":        true,
		"greengrass-support":    true,
		"kernel-module-control": true,
		"kubernetes-support":    true,
		"lxd-support":           true,
		"snapd-control":         true,
		"udisks2":               true,
		"unity8":                true,
		"wayland":               true,
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

	err = cand.CheckAutoConnect()
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
		err = cand.CheckAutoConnect()
		c.Check(err, checker)
	}

	for _, plugYaml := range opts.readonlyYamls {
		checkOpticalDriveAutoConnect(plugYaml, IsNil)
	}
	for _, plugYaml := range opts.writableYamls {
		checkOpticalDriveAutoConnect(plugYaml, NotNil)
	}
}
