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
)

type SecuritySuite struct {
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
	s.repo = NewRepository()
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
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/run/snappy/security/apparmor/producer/hook.profile": []byte("" +
			"fake \"/snaps/producer/current/hook\" {\n" +
			"producer snippet\n" +
			"}\n"),
	})
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
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/run/snappy/security/apparmor/consumer/app.profile": []byte("" +
			"fake \"/snaps/consumer/current/app\" {\n" +
			"consumer snippet\n" +
			"}\n"),
	})
}

// Tests for secComp

func (s *SecuritySuite) TestSecCompPlugPermissions(c *C) {
	s.prepareFixtureWithInterface(c, &TestInterface{
		InterfaceName: "interface",
		PlugSnippetCallback: func(plug *Plug, slot *Slot, securitySystem SecuritySystem) ([]byte, error) {
			if securitySystem == SecuritySecComp {
				return []byte("allow open\n"), nil
			}
			return nil, nil
		},
	})
	// Ensure that plug-side security profile looks correct.
	blobs, err := s.repo.SecurityFilesForSnap(s.plug.Snap)
	c.Assert(err, IsNil)
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/run/snappy/security/seccomp/producer/hook.profile": []byte("" +
			"# TODO: add default seccomp profile here\n" +
			"allow open\n"),
	})
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
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/run/snappy/security/seccomp/consumer/app.profile": []byte("" +
			"# TODO: add default seccomp profile here\n" +
			"deny kexec\n"),
	})
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
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/etc/udev/rules.d/70-snappy-producer.rules": []byte("...\n"),
	})
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
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/etc/udev/rules.d/70-snappy-consumer.rules": []byte("...\n"),
	})
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
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/etc/dbus-1/system.d/producer.conf": []byte("" +
			"<!DOCTYPE busconfig PUBLIC\n" +
			" \"-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN\"\n" +
			" \"http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd\">\n" +
			"<busconfig>\n" +
			"...\n" +
			"</busconfig>\n"),
	})
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
	c.Check(blobs, DeepEquals, map[string][]byte{
		"/etc/dbus-1/system.d/consumer.conf": []byte("" +
			"<!DOCTYPE busconfig PUBLIC\n" +
			" \"-//freedesktop//DTD D-BUS Bus Configuration 1.0//EN\"\n" +
			" \"http://www.freedesktop.org/standards/dbus/1.0/busconfig.dtd\">\n" +
			"<busconfig>\n" +
			"...\n" +
			"</busconfig>\n"),
	})
}
