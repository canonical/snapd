// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package keyboard_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dbusutil/dbustest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/keyboard"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type xkbTestSuite struct {
	testutil.BaseTest

	rootDir string

	mockDBusProperties map[string]any
}

var _ = Suite(&xkbTestSuite{})

func (s *xkbTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.mockDBusProperties = nil

	systemBus, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		c.Assert(msg.Type, Equals, dbus.TypeMethodCall)
		c.Assert(msg.Flags, Equals, dbus.Flags(0))
		c.Assert(msg.Headers, DeepEquals, map[dbus.HeaderField]dbus.Variant{
			dbus.FieldPath:        dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/locale1")),
			dbus.FieldInterface:   dbus.MakeVariant("org.freedesktop.DBus.Properties"),
			dbus.FieldMember:      dbus.MakeVariant("GetAll"),
			dbus.FieldDestination: dbus.MakeVariant("org.freedesktop.locale1"),
			dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf("")),
		})
		c.Assert(msg.Body, DeepEquals, []any{"org.freedesktop.locale1"})

		properties := make(map[string]dbus.Variant, len(s.mockDBusProperties))
		for property, val := range s.mockDBusProperties {
			properties[property] = dbus.MakeVariant(val)
		}
		resp := &dbus.Message{
			Type: dbus.TypeMethodReply,
			Headers: map[dbus.HeaderField]dbus.Variant{
				dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
				dbus.FieldPath:        dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/locale1")),
				dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf(map[string]dbus.Variant{})),
			},
			Body: []any{properties},
		}
		return []*dbus.Message{resp}, nil
	})
	c.Assert(err, IsNil)

	s.AddCleanup(dbusutil.MockOnlySystemBusAvailable(systemBus))
}

func (s *xkbTestSuite) TestXKBConfigKernelCommandLineValue(c *C) {
	config := keyboard.XKBConfig{
		Model:    "pc105",
		Variants: []string{"", "bksl", ""},
		Layouts:  []string{"us", "cz", "de"},
		Options:  []string{"grp:alt_shift_toggle", "terminate:ctrl_alt_bksp"},
	}
	c.Assert(config.KernelCommandLineValue(), Equals, "us,pc105,,grp:alt_shift_toggle,terminate:ctrl_alt_bksp")
}

func (s *xkbTestSuite) TestCurrentXKBConfig(c *C) {
	s.mockDBusProperties = map[string]any{
		"X11Model":   "pc105",
		"X11Layout":  "us,cz,de",
		"X11Variant": ",bksl,",
		"X11Options": "grp:alt_shift_toggle,terminate:ctrl_alt_bksp",
	}

	config, err := keyboard.CurrentXKBConfig()
	c.Assert(err, IsNil)
	c.Assert(config, DeepEquals, &keyboard.XKBConfig{
		Model:    "pc105",
		Variants: []string{"", "bksl", ""},
		Layouts:  []string{"us", "cz", "de"},
		Options:  []string{"grp:alt_shift_toggle", "terminate:ctrl_alt_bksp"},
	})
	c.Assert(config.KernelCommandLineValue(), Equals, "us,pc105,,grp:alt_shift_toggle,terminate:ctrl_alt_bksp")

	// defaults
	s.mockDBusProperties = map[string]any{
		"X11Model":   "",
		"X11Layout":  "",
		"X11Variant": "",
		"X11Options": "",
	}

	config, err = keyboard.CurrentXKBConfig()
	c.Assert(err, IsNil)
	c.Assert(config, DeepEquals, &keyboard.XKBConfig{
		Model:    "",
		Variants: nil,
		Layouts:  nil,
		Options:  nil,
	})
	c.Assert(config.KernelCommandLineValue(), Equals, ",,,")
}

func (s *xkbTestSuite) TestCurrentXKBConfigErrors(c *C) {
	type testcase struct {
		properties  map[string]any
		expectedErr string
	}

	tcs := []testcase{
		{
			properties: map[string]any{
				"X11Model":   "pc105,pc104",
				"X11Layout":  "",
				"X11Variant": "",
				"X11Options": "",
			},
			expectedErr: `cannot parse XKB configuration: model cannot contain ',': found "pc105,pc104"`,
		},
		{
			properties: map[string]any{
				"X11Model":   "pc105",
				"X11Layout":  "cz,us",
				"X11Variant": "bksl",
				"X11Options": "",
			},
			expectedErr: `cannot parse XKB configuration: layouts and variants do not have the same length`,
		},
		{
			properties: map[string]any{
				"X11Model":  "pc105",
				"X11Layout": "us",
				// If X11Variant is set, The length check should fail.
				"X11Variant": ",bksl",
				"X11Options": "",
			},
			expectedErr: `cannot parse XKB configuration: layouts and variants do not have the same length`,
		},
		{
			properties: map[string]any{
				"X11Model":  "pc105",
				"X11Layout": "us,cz,de",
				// If X11Variant is unset, the length check is ignored and
				// the variant defaults to "basic".
				"X11Variant": "",
				"X11Options": "",
			},
		},
		{
			properties: map[string]any{
				"X11Model":   1,
				"X11Layout":  "",
				"X11Variant": "",
				"X11Options": "",
			},
			expectedErr: `internal error: expected type string for "X11Model", found int32`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("tc[%d] failed", i)
		s.mockDBusProperties = tc.properties
		config, err := keyboard.CurrentXKBConfig()
		if tc.expectedErr != "" {
			c.Check(err, ErrorMatches, tc.expectedErr, cmt)
			c.Check(config, IsNil, cmt)
		} else {
			c.Check(err, IsNil, cmt)
		}
	}
}

func (s *xkbTestSuite) TestXKBConfigListener(c *C) {
	vconsoleConfPath := filepath.Join(s.rootDir, "/etc/vconsole.conf")
	kbConfPath := filepath.Join(s.rootDir, "/etc/default/keyboard")
	c.Assert(os.MkdirAll(filepath.Dir(kbConfPath), 0o755), IsNil)
	// mock /etc/default/keyboard as a symlink to /etc/vconsole.conf
	c.Assert(os.WriteFile(vconsoleConfPath, nil, 0o644), IsNil)
	c.Assert(os.Symlink(vconsoleConfPath, kbConfPath), IsNil)

	s.mockDBusProperties = map[string]any{
		"X11Model":   "pc105",
		"X11Layout":  "us,cz,de",
		"X11Variant": ",bksl,",
		"X11Options": "grp:alt_shift_toggle,terminate:ctrl_alt_bksp",
	}

	ctx, cancel := context.WithCancel(context.Background())

	called := 0
	cbChan := make(chan bool)
	cb := func(config *keyboard.XKBConfig) {
		called++
		c.Assert(config, DeepEquals, &keyboard.XKBConfig{
			Model:    "pc105",
			Variants: []string{"", "bksl", ""},
			Layouts:  []string{"us", "cz", "de"},
			Options:  []string{"grp:alt_shift_toggle", "terminate:ctrl_alt_bksp"},
		})
		cbChan <- true
	}
	listener, err := keyboard.NewXKBConfigListener(ctx, cb)
	c.Assert(err, IsNil)
	c.Check(listener, NotNil)

	c.Assert(os.WriteFile(kbConfPath, []byte("1"), 0o644), IsNil)
	<-cbChan
	c.Assert(os.WriteFile(kbConfPath, []byte("2"), 0o644), IsNil)
	<-cbChan
	c.Assert(os.WriteFile(vconsoleConfPath, []byte("3"), 0o644), IsNil)
	<-cbChan
	// Simulate replacement i.e. rename
	c.Assert(osutil.AtomicWriteFile(vconsoleConfPath, []byte("4"), 0644, 0), IsNil)
	<-cbChan
	c.Assert(os.WriteFile(vconsoleConfPath, []byte("5"), 0o644), IsNil)
	<-cbChan
	c.Assert(os.WriteFile(vconsoleConfPath, []byte("6"), 0o644), IsNil)
	<-cbChan

	c.Assert(called, Equals, 6)

	cancel()
	// Wait to make sure we don't accidentally check number of calls
	// before inotify gets a chance to detect the event.
	time.Sleep(20 * time.Millisecond)
	c.Assert(os.WriteFile(vconsoleConfPath, []byte("4"), 0o644), IsNil)
	// Context cancellation closes the inotify watcher.
	c.Assert(called, Equals, 6)
}
