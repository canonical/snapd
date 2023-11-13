// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package snapstate_test

import (
	"context"
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/snap"
)

const (
	dbusSessionYamlTemplate = `name: %s
slots:
  dbus-slot:
    interface: dbus
    bus: session
    name: org.example.Foo
apps:
  daemon:
    daemon: simple
    daemon-scope: user
    activates-on: [dbus-slot]
`
	dbusSystemYamlTemplate = `name: %s
slots:
  dbus-slot:
    interface: dbus
    bus: system
    name: org.example.Foo
apps:
  daemon:
    daemon: simple
    activates-on: [dbus-slot]
`
)

func (s *snapmgrTestSuite) TestCheckDBusServiceConflictsSystem(c *C) {
	someSnap, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(dbusSystemYamlTemplate, "some-snap")))
	c.Assert(err, IsNil)
	otherSnap, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(dbusSystemYamlTemplate, "other-snap")))
	c.Assert(err, IsNil)

	restore := snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		switch name {
		case "some-snap":
			return someSnap, nil
		case "other-snap":
			return otherSnap, nil
		default:
			return s.fakeBackend.ReadInfo(name, si)
		}
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "other-snap",
		Revision: snap.R(-42),
	}
	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})

	err = snapstate.CheckDBusServiceConflicts(s.state, someSnap)
	c.Assert(err, ErrorMatches, `snap "some-snap" requesting to activate on system bus name "org.example.Foo" conflicts with snap "other-snap" use`)
}

func (s *snapmgrTestSuite) TestCheckDBusServiceConflictsSession(c *C) {
	someSnap, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(dbusSessionYamlTemplate, "some-snap")))
	c.Assert(err, IsNil)
	otherSnap, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(dbusSessionYamlTemplate, "other-snap")))
	c.Assert(err, IsNil)

	restore := snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		switch name {
		case "some-snap":
			return someSnap, nil
		case "other-snap":
			return otherSnap, nil
		default:
			return s.fakeBackend.ReadInfo(name, si)
		}
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()
	si := &snap.SideInfo{
		RealName: "other-snap",
		Revision: snap.R(-42),
	}
	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})

	err = snapstate.CheckDBusServiceConflicts(s.state, someSnap)
	c.Assert(err, ErrorMatches, `snap "some-snap" requesting to activate on session bus name "org.example.Foo" conflicts with snap "other-snap" use`)
}

func (s *snapmgrTestSuite) TestCheckDBusServiceConflictsDifferentBuses(c *C) {
	sessionSnap, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(dbusSessionYamlTemplate, "session-snap")))
	c.Assert(err, IsNil)
	systemSnap, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(dbusSystemYamlTemplate, "system-snap")))
	c.Assert(err, IsNil)

	restore := snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		switch name {
		case "session-snap":
			return sessionSnap, nil
		case "system-snap":
			return systemSnap, nil
		default:
			return s.fakeBackend.ReadInfo(name, si)
		}
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// A snap claiming a name on the system bus does not conflict
	// with a snap providing the same name on the session bus.
	si := &snap.SideInfo{
		RealName: "system-snap",
		Revision: snap.R(-42),
	}
	snapstate.Set(s.state, "system-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})
	err = snapstate.CheckDBusServiceConflicts(s.state, sessionSnap)
	c.Check(err, IsNil)

	// ... and the reverse
	snapstate.Set(s.state, "system-snap", nil)
	si = &snap.SideInfo{
		RealName: "session-snap",
		Revision: snap.R(-42),
	}
	snapstate.Set(s.state, "session-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})
	err = snapstate.CheckDBusServiceConflicts(s.state, systemSnap)
	c.Check(err, IsNil)
}

func (s *snapmgrTestSuite) TestCheckDBusServiceConflictsNoConflictWithSelf(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(dbusSessionYamlTemplate, "some-snap")))
	c.Assert(err, IsNil)
	restore := snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		switch name {
		case "some-snap":
			return info, nil
		default:
			return s.fakeBackend.ReadInfo(name, si)
		}
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// No conflicts on first installation
	err = snapstate.CheckDBusServiceConflicts(s.state, info)
	c.Assert(err, IsNil)

	// Snap does not conflict against itself
	si := &snap.SideInfo{
		RealName: "some-snap",
		Revision: snap.R(-42),
	}
	snapstate.Set(s.state, "some-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})
	err = snapstate.CheckDBusServiceConflicts(s.state, info)
	c.Assert(err, IsNil)
}

func (s *snapmgrTestSuite) TestInstallDBusActivationConflicts(c *C) {
	someSnap, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(dbusSystemYamlTemplate, "some-snap")))
	c.Assert(err, IsNil)
	otherSnap, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(dbusSystemYamlTemplate, "other-snap")))
	c.Assert(err, IsNil)

	restore := snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		switch name {
		case "some-snap":
			return someSnap, nil
		case "other-snap":
			return otherSnap, nil
		default:
			return s.fakeBackend.ReadInfo(name, si)
		}
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	si := &snap.SideInfo{
		RealName: "other-snap",
		Revision: snap.R(-42),
	}
	snapstate.Set(s.state, "other-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.dbus-activation", true)
	tr.Commit()

	opts := &snapstate.RevisionOptions{Channel: "channel-for-dbus-activation"}
	_, err = snapstate.Install(context.Background(), s.state, "some-snap", opts, s.user.ID, snapstate.Flags{})
	c.Check(err, ErrorMatches, `snap "some-snap" requesting to activate on system bus name "org.example.Foo" conflicts with snap "other-snap" use`)
}

func (s *snapmgrTestSuite) TestInstallManyDBusActivationConflicts(c *C) {
	someSnap, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(dbusSystemYamlTemplate, "some-snap")))
	c.Assert(err, IsNil)
	otherSnap, err := snap.InfoFromSnapYaml([]byte(fmt.Sprintf(dbusSystemYamlTemplate, "other-snap")))
	c.Assert(err, IsNil)

	restore := snapstate.MockSnapReadInfo(func(name string, si *snap.SideInfo) (*snap.Info, error) {
		switch name {
		case "some-snap":
			return someSnap, nil
		case "other-snap":
			return otherSnap, nil
		default:
			return s.fakeBackend.ReadInfo(name, si)
		}
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.dbus-activation", true)
	tr.Commit()

	snapNames := []string{"some-snap", "other-snap"}
	_, tss, err := snapstate.InstallMany(s.state, snapNames, nil, s.user.ID, nil)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("install", "install two snaps")
	for _, ts := range tss {
		chg.AddAll(ts)
	}

	s.settle(c)

	// The order of installation is indeterminant, but one will fail
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:\n- Make snap "(some|other)-snap" \(11\) available to the system \(snap "(some|other)-snap" requesting to activate on system bus name "org.example.Foo" conflicts with snap "(some|other)-snap" use\)`)
}
