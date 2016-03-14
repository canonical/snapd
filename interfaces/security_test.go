// -*- Mote: Go; indent-tabs-mode: t -*-

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

package interfaces_test

import (
	. "gopkg.in/check.v1"

	. "github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/testutil"
)

type SecuritySuite struct {
	testutil.BaseTest
	repo *Repository
	plug *Plug
	slot *Slot
}

var _ = Suite(&SecuritySuite{
	plug: &Plug{
		Snap:      "producer",
		Name:      "plug",
		Interface: "interface",
		Apps:      []string{"hook"},
	},
	slot: &Slot{
		Snap:      "consumer",
		Name:      "slot",
		Interface: "interface",
		Apps:      []string{"app"},
	},
})

func (s *SecuritySuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.repo = NewRepository()
	MockActiveSnapMetaData(&s.BaseTest, func(snapName string) (string, string, []string, error) {
		switch snapName {
		case "producer":
			return "version", "origin", []string{"hook"}, nil
		case "consumer":
			return "version", "origin", []string{"app"}, nil
		default:
			panic("unexpected snap name")
		}
	})
	MockSecCompHeader(&s.BaseTest, []byte("# Mocked seccomp header\n"))
	MockAppArmorHeader(&s.BaseTest, []byte(""+
		"###VAR###\n"+
		"###PROFILEATTACH### {\n"))
}

func (s *SecuritySuite) prepareFixtureWithInterface(c *C, i Interface) {
	err := s.repo.AddInterface(i)
	c.Assert(err, IsNil)
	err = s.repo.AddPlug(s.plug)
	c.Assert(err, IsNil)
	err = s.repo.AddSlot(s.slot)
	c.Assert(err, IsNil)
	err = s.repo.Connect(s.plug.Snap, s.plug.Name, s.slot.Snap, s.slot.Name)
	c.Assert(err, IsNil)
}

// Tests for WrapperNameForApp()

func (s *SecuritySuite) TestWrapperNameForApp(c *C) {
	c.Assert(WrapperNameForApp("snap", "app"), Equals, "snap.app")
	c.Assert(WrapperNameForApp("foo", "foo"), Equals, "foo")
}

// Tests for SecurityTagForApp()

func (s *SecuritySuite) TestSecurityTagForApp(c *C) {
	c.Assert(SecurityTagForApp("snap", "app"), Equals, "snap.app.snap")
	c.Assert(SecurityTagForApp("foo", "foo"), Equals, "foo.snap")
}

// Tests for appArmor

func (s *SecuritySuite) TestAppArmorPlugPermissions(c *C) {
	s.prepareFixtureWithInterface(c, &TestInterface{
		InterfaceName: "interface",
		PlugSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecurityAppArmor {
				return []byte("producer snippet\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that plug-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.plug.Snap)
	c.Assert(err, IsNil)
	c.Check(string(blobs["/var/lib/snappy/apparmor/profiles/producer.hook.snap"]), DeepEquals, `
# Specified profile variables
@{APP_APPNAME}="hook"
@{APP_ID_DBUS}="producer_2eorigin_5fhook_5fversion"
@{APP_PKGNAME_DBUS}="producer_2eorigin"
@{APP_PKGNAME}="producer.origin"
@{APP_VERSION}="version"
@{INSTALL_DIR}="{/snaps,/gadget}"
profile "producer.hook.snap" {
producer snippet
}
`)
}

func (s *SecuritySuite) TestAppArmorSlotPermissions(c *C) {
	s.prepareFixtureWithInterface(c, &TestInterface{
		InterfaceName: "interface",
		SlotSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecurityAppArmor {
				return []byte("consumer snippet\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that slot-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.slot.Snap)
	c.Assert(err, IsNil)
	c.Check(string(blobs["/var/lib/snappy/apparmor/profiles/consumer.app.snap"]), DeepEquals, `
# Specified profile variables
@{APP_APPNAME}="app"
@{APP_ID_DBUS}="consumer_2eorigin_5fapp_5fversion"
@{APP_PKGNAME_DBUS}="consumer_2eorigin"
@{APP_PKGNAME}="consumer.origin"
@{APP_VERSION}="version"
@{INSTALL_DIR}="{/snaps,/gadget}"
profile "consumer.app.snap" {
consumer snippet
}
`)
}

// Tests for secComp

func (s *SecuritySuite) TestSecCompPlugPermissions(c *C) {
	s.prepareFixtureWithInterface(c, &TestInterface{
		InterfaceName: "interface",
		PlugSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecuritySecComp {
				return []byte("open\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that plug-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.plug.Snap)
	c.Assert(err, IsNil)
	c.Check(blobs["/var/lib/snappy/seccomp/profiles/producer.hook.snap"], DeepEquals, []byte(""+
		"# Mocked seccomp header\n"+
		"open\n"))
}

func (s *SecuritySuite) TestSecCompSlotPermissions(c *C) {
	s.prepareFixtureWithInterface(c, &TestInterface{
		InterfaceName: "interface",
		SlotSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecuritySecComp {
				return []byte("deny kexec\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that slot-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.slot.Snap)
	c.Assert(err, IsNil)
	c.Check(blobs["/var/lib/snappy/seccomp/profiles/consumer.app.snap"], DeepEquals, []byte(""+
		"# Mocked seccomp header\n"+
		"deny kexec\n"))
}

// Tests for uDev

func (s *SecuritySuite) TestUdevPlugPermissions(c *C) {
	s.prepareFixtureWithInterface(c, &TestInterface{
		InterfaceName: "interface",
		PlugSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecurityUDev {
				return []byte("...\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that plug-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.plug.Snap)
	c.Assert(err, IsNil)
	c.Check(blobs["/etc/udev/rules.d/70-producer.hook.snap.rules"], DeepEquals, []byte("...\n"))
}

func (s *SecuritySuite) TestUdevSlotPermissions(c *C) {
	s.prepareFixtureWithInterface(c, &TestInterface{
		InterfaceName: "interface",
		SlotSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecurityUDev {
				return []byte("...\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that slot-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.slot.Snap)
	c.Assert(err, IsNil)
	c.Check(blobs["/etc/udev/rules.d/70-consumer.app.snap.rules"], DeepEquals, []byte("...\n"))
}

// Tests for DBus

func (s *SecuritySuite) TestDBusPlugPermissions(c *C) {
	s.prepareFixtureWithInterface(c, &TestInterface{
		InterfaceName: "interface",
		PlugSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecurityDBus {
				return []byte("...\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that plug-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.plug.Snap)
	c.Assert(err, IsNil)
	c.Check(blobs["/etc/dbus-1/system.d/producer.hook.snap.conf"], DeepEquals, []byte(""+
		"<!DOCTYPE busconfig PUBLIC\n"+
		" \"-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN\"\n"+
		" \"http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd\">\n"+
		"<busconfig>\n"+
		"...\n"+
		"</busconfig>\n"))
}

func (s *SecuritySuite) TestDBusSlotPermissions(c *C) {
	s.prepareFixtureWithInterface(c, &TestInterface{
		InterfaceName: "interface",
		SlotSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecurityDBus {
				return []byte("...\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that slot-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.slot.Snap)
	c.Assert(err, IsNil)
	c.Check(blobs["/etc/dbus-1/system.d/consumer.app.snap.conf"], DeepEquals, []byte(""+
		"<!DOCTYPE busconfig PUBLIC\n"+
		" \"-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN\"\n"+
		" \"http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd\">\n"+
		"<busconfig>\n"+
		"...\n"+
		"</busconfig>\n"))
}
