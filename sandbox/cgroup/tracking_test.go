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

package cgroup_test

import (
	"fmt"
	"io/ioutil"
	"os"

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dbusutil/dbustest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/testutil"
)

func enableFeatures(c *C, ff ...features.SnapdFeature) {
	c.Assert(os.MkdirAll(dirs.FeaturesDir, 0755), IsNil)
	for _, f := range ff {
		c.Assert(ioutil.WriteFile(f.ControlFile(), nil, 0755), IsNil)
	}
}

type trackingSuite struct{}

var _ = Suite(&trackingSuite{})

func (s *trackingSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *trackingSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

// CreateTransientScope is a no-op when refresh app awareness is off
func (s *trackingSuite) TestCreateTransientScopeFeatureDisabled(c *C) {
	noDBus := func() (*dbus.Conn, error) {
		return nil, fmt.Errorf("dbus should not have been used")
	}
	restore := dbusutil.MockConnections(noDBus, noDBus)
	defer restore()

	c.Assert(features.RefreshAppAwareness.IsEnabled(), Equals, false)
	err := cgroup.CreateTransientScope("snap.pkg.app")
	c.Check(err, IsNil)
}

// CreateTransientScope does stuff when refresh app awareness is on
func (s *trackingSuite) TestCreateTransientScopeFeatureEnabled(c *C) {
	// Pretend that refresh app awareness is enabled
	enableFeatures(c, features.RefreshAppAwareness)
	c.Assert(features.RefreshAppAwareness.IsEnabled(), Equals, true)
	// Pretend we are a non-root user so that session bus is used.
	restore := cgroup.MockOsGetuid(12345)
	defer restore()
	// Pretend our PID is this value.
	restore = cgroup.MockOsGetpid(312123)
	defer restore()
	// Rig the random UUID generator to return this value.
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	restore = cgroup.MockRandomUUID(uuid)
	defer restore()
	// Replace interactions with DBus so that only session bus is available and responds with our logic.
	conn, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			return []*dbus.Message{happyResponseToStartTransientUnit(c, msg, "snap.pkg.app."+uuid+".scope", 312123)}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	c.Assert(err, IsNil)
	restore = dbusutil.MockSessionBus(conn)
	defer restore()
	// Replace the cgroup analyzer function
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		return "/user.slice/user-12345.slice/user@12345.service/snap.pkg.app." + uuid + ".scope", nil
	})
	defer restore()

	err = cgroup.CreateTransientScope("snap.pkg.app")
	c.Check(err, IsNil)
}

func happyResponseToStartTransientUnit(c *C, msg *dbus.Message, scopeName string, pid int) *dbus.Message {
	// XXX: Those types might live in a package somewhere
	type Property struct {
		Name  string
		Value interface{}
	}
	type Unit struct {
		Name  string
		Props []Property
	}
	// Signature of StartTransientUnit, string, string, array of Property and array of Unit (see above).
	requestSig := dbus.SignatureOf("", "", []Property{}, []Unit{})

	c.Assert(msg.Type, Equals, dbus.TypeMethodCall)
	c.Check(msg.Flags, Equals, dbus.Flags(0))
	c.Check(msg.Headers, DeepEquals, map[dbus.HeaderField]dbus.Variant{
		dbus.FieldDestination: dbus.MakeVariant("org.freedesktop.systemd1"),
		dbus.FieldPath:        dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/systemd1")),
		dbus.FieldInterface:   dbus.MakeVariant("org.freedesktop.systemd1.Manager"),
		dbus.FieldMember:      dbus.MakeVariant("StartTransientUnit"),
		dbus.FieldSignature:   dbus.MakeVariant(requestSig),
	})
	c.Check(msg.Body, DeepEquals, []interface{}{
		scopeName,
		"fail",
		[][]interface{}{
			{"PIDs", dbus.MakeVariant([]uint32{uint32(pid)})},
		},
		[][]interface{}{},
	})

	responseSig := dbus.SignatureOf(dbus.ObjectPath(""))
	return &dbus.Message{
		Type: dbus.TypeMethodReply,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
			dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
			// dbus.FieldDestination is provided automatically by DBus test helper.
			dbus.FieldSignature: dbus.MakeVariant(responseSig),
		},
		// The object path returned in the body is not used by snap run yet.
		Body: []interface{}{dbus.ObjectPath("/org/freedesktop/systemd1/job/1462")},
	}
}

func unhappyResponseToStartTransientUnit(c *C, msg *dbus.Message, errMsg string) *dbus.Message {
	c.Assert(msg.Type, Equals, dbus.TypeMethodCall)
	// ignore the message and just produce an error response
	return &dbus.Message{
		Type: dbus.TypeError,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
			dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
			// dbus.FieldDestination is provided automatically by DBus test helper.
			dbus.FieldErrorName: dbus.MakeVariant(errMsg),
		},
	}
}

func (s *trackingSuite) TestDoCreateTransientScopeHappy(c *C) {
	conn, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			return []*dbus.Message{happyResponseToStartTransientUnit(c, msg, "foo.scope", 312123)}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})

	c.Assert(err, IsNil)
	defer conn.Close()
	err = cgroup.DoCreateTransientScope(conn, "foo.scope", 312123)
	c.Assert(err, IsNil)
}

func (s *trackingSuite) TestDoCreateTransientScopeForwardedErrors(c *C) {
	// Certain errors are forwareded and handled in the logic calling into
	// DoCreateTransientScope. Those are tested here.
	for _, errMsg := range []string{
		"org.freedesktop.DBus.Error.NameHasNoOwner",
		"org.freedesktop.DBus.Error.UnknownMethod",
		"org.freedesktop.DBus.Error.Spawn.ChildExited",
	} {
		conn, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
			switch n {
			case 0:
				return []*dbus.Message{unhappyResponseToStartTransientUnit(c, msg, errMsg)}, nil
			}
			return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
		})
		c.Assert(err, IsNil)
		defer conn.Close()
		err = cgroup.DoCreateTransientScope(conn, "foo.scope", 312123)
		c.Assert(err, ErrorMatches, errMsg)
	}
}

func (s *trackingSuite) TestDoCreateTransientScopeClashingScopeName(c *C) {
	// In case our UUID algorithm is bad and systemd reports that an unit with
	// identical name already exists, we provide a special error handler for that.
	errMsg := "org.freedesktop.systemd1.UnitExists"
	conn, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			return []*dbus.Message{unhappyResponseToStartTransientUnit(c, msg, errMsg)}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	c.Assert(err, IsNil)
	defer conn.Close()
	err = cgroup.DoCreateTransientScope(conn, "foo.scope", 312123)
	c.Assert(err, ErrorMatches, "cannot create transient scope: scope .* clashed: .*")
}

func (s *trackingSuite) TestDoCreateTransientScopeOtherDBusErrors(c *C) {
	// Other DBus errors are not special-cased and cause a generic failure handler.
	errMsg := "org.example.BadHairDay"
	conn, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			return []*dbus.Message{unhappyResponseToStartTransientUnit(c, msg, errMsg)}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	c.Assert(err, IsNil)
	defer conn.Close()
	err = cgroup.DoCreateTransientScope(conn, "foo.scope", 312123)
	c.Assert(err, ErrorMatches, `cannot create transient scope: DBus error "org.example.BadHairDay": \[\]`)
}

// stubReadWriteCloser implements ReadWriteCloser for dbus.NewConn
type stubReadWriteCloser struct{}

func (*stubReadWriteCloser) Read(p []byte) (n int, err error) {
	return 0, nil
}

func (*stubReadWriteCloser) Write(p []byte) (n int, err error) {
	return 0, nil
}

func (*stubReadWriteCloser) Close() error {
	return nil
}

// stubBusConnection returns a dbus connection for the tests below.
//
// Using dbustest.Connection panics as the goroutines spawned by
// go-dbus do not expect the connection to be immediatley closed.
func stubBusConnection() (*dbus.Conn, error) {
	return dbus.NewConn(&stubReadWriteCloser{})
}

func (s *trackingSuite) TestSessionOrMaybeSystemBusTotalFailureForRoot(c *C) {
	system := func() (*dbus.Conn, error) {
		return nil, fmt.Errorf("system bus unavailable for testing")
	}
	session := func() (*dbus.Conn, error) {
		return nil, fmt.Errorf("session bus unavailable for testing")
	}
	restore := dbusutil.MockConnections(system, session)
	defer restore()
	logBuf, restore := logger.MockLogger()
	defer restore()
	os.Setenv("SNAPD_DEBUG", "true")
	defer os.Unsetenv("SNAPD_DEBUG")

	uid := 0
	isSession, conn, err := cgroup.SessionOrMaybeSystemBus(uid)
	c.Assert(err, ErrorMatches, "system bus unavailable for testing")
	c.Check(conn, IsNil)
	c.Check(isSession, Equals, false)
	c.Check(logBuf.String(), testutil.Contains, "DEBUG: session bus is not available: session bus unavailable for testing\n")
	c.Check(logBuf.String(), testutil.Contains, "DEBUG: falling back to system bus\n")
	c.Check(logBuf.String(), testutil.Contains, "DEBUG: system bus is not available: system bus unavailable for testing\n")
}

func (s *trackingSuite) TestSessionOrMaybeSystemBusFallbackForRoot(c *C) {
	system := func() (*dbus.Conn, error) {
		return stubBusConnection()
	}
	session := func() (*dbus.Conn, error) {
		return nil, fmt.Errorf("session bus unavailable for testing")
	}
	restore := dbusutil.MockConnections(system, session)
	defer restore()
	logBuf, restore := logger.MockLogger()
	defer restore()
	os.Setenv("SNAPD_DEBUG", "true")
	defer os.Unsetenv("SNAPD_DEBUG")

	uid := 0
	isSession, conn, err := cgroup.SessionOrMaybeSystemBus(uid)
	c.Assert(err, IsNil)
	conn.Close()
	c.Check(isSession, Equals, false)
	c.Check(logBuf.String(), testutil.Contains, "DEBUG: session bus is not available: session bus unavailable for testing\n")
	c.Check(logBuf.String(), testutil.Contains, "DEBUG: falling back to system bus\n")
	c.Check(logBuf.String(), testutil.Contains, "DEBUG: using system bus now, session bus was not available\n")
}

func (s *trackingSuite) TestSessionOrMaybeSystemBusNonRootSessionFailure(c *C) {
	system := func() (*dbus.Conn, error) {
		return stubBusConnection()
	}
	session := func() (*dbus.Conn, error) {
		return nil, fmt.Errorf("session bus unavailable for testing")
	}
	restore := dbusutil.MockConnections(system, session)
	defer restore()
	logBuf, restore := logger.MockLogger()
	defer restore()
	os.Setenv("SNAPD_DEBUG", "true")
	defer os.Unsetenv("SNAPD_DEBUG")

	uid := 12345
	isSession, conn, err := cgroup.SessionOrMaybeSystemBus(uid)
	c.Assert(err, ErrorMatches, "session bus unavailable for testing")
	c.Check(conn, IsNil)
	c.Check(isSession, Equals, false)
	c.Check(logBuf.String(), testutil.Contains, "DEBUG: session bus is not available: session bus unavailable for testing\n")
}
