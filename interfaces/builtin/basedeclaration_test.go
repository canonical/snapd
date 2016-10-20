// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package builtin_test

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
)

type baseDeclSuite struct {
	baseDecl *asserts.BaseDeclaration
}

var _ = Suite(&baseDeclSuite{})

func (s *baseDeclSuite) SetUpSuite(c *C) {
	s.baseDecl = asserts.BuiltinBaseDeclaration()
}

func (s *baseDeclSuite) connectCand(c *C, iface, slotYaml, plugYaml string) *policy.ConnectCandidate {
	if slotYaml == "" {
		slotYaml = fmt.Sprintf(`name: slot-snap
slots:
  %s:
`, iface)
	}
	if plugYaml == "" {
		plugYaml = fmt.Sprintf(`name: plug-snap
plugs:
  %s:
`, iface)
	}
	slotSnap := snaptest.MockInfo(c, slotYaml, nil)
	plugSnap := snaptest.MockInfo(c, plugYaml, nil)
	return &policy.ConnectCandidate{
		Plug:            plugSnap.Plugs[iface],
		Slot:            slotSnap.Slots[iface],
		BaseDeclaration: s.baseDecl,
	}
}

func (s *baseDeclSuite) installSlotCand(c *C, iface string, snapType snap.Type, yaml string) *policy.InstallCandidate {
	if yaml == "" {
		yaml = fmt.Sprintf(`name: install-slot-snap
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
		"home":          true,
		"lxd-support":   true,
		"snapd-control": true,
	}

	// these simply auto-connect, anything else doesn't
	autoconnect := map[string]bool{
		"browser-support":        true,
		"gsettings":              true,
		"mir":                    true,
		"network":                true,
		"network-bind":           true,
		"opengl":                 true,
		"optical-drive":          true,
		"pulseaudio":             true,
		"screen-inhibit-control": true,
		"unity7":                 true,
		"upower-observe":         true,
		"x11":                    true,
	}

	for _, iface := range all {
		if snowflakes[iface.Name()] {
			continue
		}
		expected := autoconnect[iface.Name()]
		comm := Commentf(iface.Name())

		// cross-check with past behavior
		c.Check(expected, Equals, iface.LegacyAutoConnect(), comm)

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
		"content":     true,
		"home":        true,
		"lxd-support": true,
	}

	for _, iface := range all {
		if snowflakes[iface.Name()] {
			continue
		}
		c.Check(iface.AutoConnect(nil, nil), Equals, true)
	}
}

func (s *baseDeclSuite) TestInterimAutoConnectionHome(c *C) {
	r1 := release.MockOnClassic(true)
	defer r1()
	cand := s.connectCand(c, "home", "", "")
	err := cand.CheckAutoConnect()
	c.Check(err, IsNil)

	release.OnClassic = false
	err = cand.CheckAutoConnect()
	c.Check(err, ErrorMatches, `auto-connection denied by slot rule of interface \"home\"`)
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
	// content will also depend for now AutoConnect(plug, slot)
	// random snaps cannot connect with content
	cand := s.connectCand(c, "content", "", "")
	err := cand.CheckAutoConnect()
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
// SanitizeSlot),
// otherwise the entry for the interface is the list of snap types it
// can be installed by (using the declaration naming);
// ATM a nil entry means even stricter rules that would need be tested
// separately and whose implementation is in flux for now
var (
	unconstrained = []string{"core", "kernel", "gadget", "app"}

	slotInstallation = map[string][]string{
		// unconstrained
		"bluez":            unconstrained,
		"fwupd":            unconstrained,
		"location-control": unconstrained,
		"location-observe": unconstrained,
		"modem-manager":    unconstrained,
		"udisks2":          unconstrained,
		// other
		"bool-file":       []string{"core", "gadget"},
		"browser-support": []string{"core"},
		"content":         []string{"app", "gadget"},
		"docker-support":  []string{"core"},
		"gpio":            []string{"core", "gadget"},
		"hidraw":          []string{"core", "gadget"},
		"lxd-support":     []string{"core"},
		"mir":             []string{"app"},
		"mpris":           []string{"app"},
		"network-manager": []string{"app", "core"},
		"ppp":             []string{"core"},
		"pulseaudio":      []string{"app", "core"},
		"serial-port":     []string{"core", "gadget"},
		// snowflakes
		"docker": nil,
	}
)

func contains(l []string, s string) bool {
	for _, s1 := range l {
		if s == s1 {
			return true
		}
	}
	return false
}

func (s *baseDeclSuite) TestSlotInstallation(c *C) {
	typMap := map[string]snap.Type{
		"core":   snap.TypeOS,
		"app":    snap.TypeApp,
		"kernel": snap.TypeKernel,
		"gadget": snap.TypeGadget,
	}

	all := builtin.Interfaces()

	for _, iface := range all {
		types, ok := slotInstallation[iface.Name()]
		compareWithSanitize := false
		if !ok { // common ones, only core can install them,
			// their plain SanitizeSlot checked for that
			types = []string{"core"}
			compareWithSanitize = true
		}
		if types == nil {
			// snowflake needs to be tested specially
			continue
		}
		for name, snapType := range typMap {
			ok := contains(types, name)
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
				sanitizeErr := iface.SanitizeSlot(&interfaces.Slot{SlotInfo: slotInfo})
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
}

func (s *baseDeclSuite) TestPlugInstallation(c *C) {
	all := builtin.Interfaces()

	restricted := map[string]bool{
		"docker-support":        true,
		"kernel-module-control": true,
		"lxd-support":           true,
		"snapd-control":         true,
	}

	for _, iface := range all {
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

func (s *baseDeclSuite) TestConnection(c *C) {
	all := builtin.Interfaces()

	// connecting with these interfaces needs to be allowed on
	// case-by-case basis
	noconnect := map[string]bool{
		"bluez":            true,
		"docker":           true,
		"fwupd":            true,
		"location-control": true,
		"location-observe": true,
		"mir":              true,
		"modem-manager":    true,
		"udisks2":          true,
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

func (s *baseDeclSuite) TestSanity(c *C) {
	all := builtin.Interfaces()

	// these interfaces have rules both for the slots and plugs side
	// given how the rules work this can be delicate,
	// listed here to make sure that was a conscious decision
	bothSides := map[string]bool{
		"docker-support":        true,
		"kernel-module-control": true,
		"lxd-support":           true,
		"snapd-control":         true,
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
