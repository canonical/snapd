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
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/godbus/dbus"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dbusutil/dbustest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

func enableFeatures(c *C, ff ...features.SnapdFeature) {
	c.Assert(os.MkdirAll(dirs.FeaturesDir, 0755), IsNil)
	for _, f := range ff {
		c.Assert(os.WriteFile(f.ControlFile(), nil, 0755), IsNil)
	}
}

type trackingSuite struct{}

var _ = Suite(&trackingSuite{})

func (s *trackingSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	cgroup.MockVersion(cgroup.V2, nil)
}

func (s *trackingSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

// CreateTransientScopeForTracking always attempts to track, even when refresh app awareness flag is off.
func (s *trackingSuite) TestCreateTransientScopeForTrackingFeatureDisabled(c *C) {
	noDBus := func() (*dbus.Conn, error) {
		return nil, fmt.Errorf("dbus not available")
	}
	restore := dbusutil.MockConnections(noDBus, noDBus)
	defer restore()

	// The feature is disabled but we still track applications. The feature
	// flag is now only observed in side snapd snap manager, while considering
	// snap refreshes.
	c.Assert(features.RefreshAppAwareness.IsEnabled(), Equals, false)
	err := cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, ErrorMatches, "cannot track application process")
}

// TestCreateTransientScopeForTrackingUUIDFailure tests the UUID error path
func (s *trackingSuite) TestCreateTransientScopeForTrackingUUIDFailure(c *C) {
	// Hand out stub connections to both the system and session bus.
	// Neither is really used here but they must appear to be available.
	restore := dbusutil.MockConnections(dbustest.StubConnection, dbustest.StubConnection)
	defer restore()

	restore = cgroup.MockRandomUUID(func() (string, error) {
		return "", errors.New("mocked uuid error")
	})
	defer restore()

	err := cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, ErrorMatches, "mocked uuid error")
}

// CreateTransientScopeForTracking does stuff when refresh app awareness is on
func (s *trackingSuite) TestCreateTransientScopeForTrackingFeatureEnabled(c *C) {
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
	restore = cgroup.MockRandomUUID(func() (string, error) {
		return uuid, nil
	})
	defer restore()
	signalChan := make(chan struct{})
	// Replace interactions with DBus so that only session bus is available and responds with our logic.
	conn, inject, err := dbustest.InjectableConnection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			return []*dbus.Message{checkSystemdSignalSubscribe(c, msg)}, nil
		case 1:
			defer func() {
				close(signalChan)
			}()
			return []*dbus.Message{checkAndRespondToStartTransientUnit(c, msg, "snap.pkg.app-"+uuid+".scope", 312123)}, nil
		case 2:
			return []*dbus.Message{checkSystemSignalUnsubscribe(c, msg)}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	go func() {
		select {
		case <-signalChan:
			inject(mockJobRemovedSignal("snap.pkg.app-"+uuid+".scope", "done"))
		}
	}()
	c.Assert(err, IsNil)
	restore = dbusutil.MockOnlySessionBusAvailable(conn)
	defer restore()
	// Replace the cgroup analyzer function
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		return "/user.slice/user-12345.slice/user@12345.service/snap.pkg.app-" + uuid + ".scope", nil
	})
	defer restore()

	err = cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Check(err, IsNil)
}

func (s *trackingSuite) TestCreateTransientScopeForTrackingUnhappyNotRootGeneric(c *C) {
	// Pretend that refresh app awareness is enabled
	enableFeatures(c, features.RefreshAppAwareness)

	// Hand out stub connections to both the system and session bus.
	// Neither is really used here but they must appear to be available.
	restore := dbusutil.MockConnections(dbustest.StubConnection, dbustest.StubConnection)
	defer restore()

	// Pretend we are a non-root user so that session bus is used.
	restore = cgroup.MockOsGetuid(12345)
	defer restore()
	// Pretend our PID is this value.
	restore = cgroup.MockOsGetpid(312123)
	defer restore()

	// Rig the cgroup analyzer to return an answer not related to the snap name.
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		return "foo", nil
	})
	defer restore()

	// Pretend that attempting to create a transient scope fails with a canned error.
	restore = cgroup.MockDoCreateTransientScope(func(conn *dbus.Conn, unitName string, pid int) error {
		return fmt.Errorf("cannot create transient scope for testing")
	})
	defer restore()

	// Create a transient scope and see it fail according to how doCreateTransientScope is rigged.
	err := cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, ErrorMatches, "cannot create transient scope for testing")

	// Calling StartTransientUnit fails with org.freedesktop.DBus.UnknownMethod error.
	// This is possible on old systemd or on deputy systemd.
	restore = cgroup.MockDoCreateTransientScope(func(conn *dbus.Conn, unitName string, pid int) error {
		return cgroup.ErrDBusUnknownMethod
	})
	defer restore()

	// Attempts to create a transient scope fail with a special error
	// indicating that we cannot track application process.
	err = cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, ErrorMatches, "cannot track application process")

	// Calling StartTransientUnit fails with org.freedesktop.DBus.Spawn.ChildExited error.
	// This is possible where we try to activate socket activate session bus
	// but it's not available OR when we try to socket activate systemd --user.
	restore = cgroup.MockDoCreateTransientScope(func(conn *dbus.Conn, unitName string, pid int) error {
		return cgroup.ErrDBusSpawnChildExited
	})
	defer restore()

	// Attempts to create a transient scope fail with a special error
	// indicating that we cannot track application process and because we are
	// not root, we do not attempt to fall back to the system bus.
	err = cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, ErrorMatches, "cannot track application process")
}

func (s *trackingSuite) TestCreateTransientScopeForTrackingUnhappyRootFallback(c *C) {
	// Pretend that refresh app awareness is enabled
	enableFeatures(c, features.RefreshAppAwareness)

	// Hand out stub connections to both the system and session bus.
	// Neither is really used here but they must appear to be available.
	restore := dbusutil.MockConnections(dbustest.StubConnection, dbustest.StubConnection)
	defer restore()

	// Pretend we are a root user so that we attempt to use the system bus as fallback.
	restore = cgroup.MockOsGetuid(0)
	defer restore()
	// Pretend our PID is this value.
	restore = cgroup.MockOsGetpid(312123)
	defer restore()

	// Rig the random UUID generator to return this value.
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	restore = cgroup.MockRandomUUID(func() (string, error) {
		return uuid, nil
	})
	defer restore()

	// Calling StartTransientUnit fails on the session and then works on the system bus.
	// This test emulates a root user falling back from the session bus to the system bus.
	n := 0
	restore = cgroup.MockDoCreateTransientScope(func(conn *dbus.Conn, unitName string, pid int) error {
		n++
		switch n {
		case 1:
			// On first try we fail. This is when we used the session bus/
			return cgroup.ErrDBusSpawnChildExited
		case 2:
			// On second try we succeed.
			return nil
		}
		panic("expected to call doCreateTransientScope at most twice")
	})
	defer restore()

	// Rig the cgroup analyzer to pretend that we got placed into the system slice.
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Assert(pid, Equals, 312123)
		return "/system.slice/snap.pkg.app-" + uuid + ".scope", nil
	})
	defer restore()

	// Attempts to create a transient scope fail with a special error
	// indicating that we cannot track application process and but because we were
	// root we attempted to fall back to the system bus.
	err := cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, IsNil)
}

func (s *trackingSuite) TestCreateTransientScopeForTrackingUnhappyRootFailedFallback(c *C) {
	// Pretend that refresh app awareness is enabled
	enableFeatures(c, features.RefreshAppAwareness)

	// Make it appear that session bus is there but system bus is not.
	noSystemBus := func() (*dbus.Conn, error) {
		return nil, fmt.Errorf("system bus is not available for testing")
	}
	restore := dbusutil.MockConnections(noSystemBus, dbustest.StubConnection)
	defer restore()

	// Pretend we are a root user so that we attempt to use the system bus as fallback.
	restore = cgroup.MockOsGetuid(0)
	defer restore()
	// Pretend our PID is this value.
	restore = cgroup.MockOsGetpid(312123)
	defer restore()

	// Rig the random UUID generator to return this value.
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	restore = cgroup.MockRandomUUID(func() (string, error) {
		return uuid, nil
	})
	defer restore()

	// Calling StartTransientUnit fails so that we try to use the system bus as fallback.
	restore = cgroup.MockDoCreateTransientScope(func(conn *dbus.Conn, unitName string, pid int) error {
		return cgroup.ErrDBusSpawnChildExited
	})
	defer restore()

	// Rig the cgroup analyzer to return an answer not related to the snap name.
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		return "foo", nil
	})
	defer restore()

	// Attempts to create a transient scope fail with a special error
	// indicating that we cannot track application process.
	err := cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, ErrorMatches, "cannot track application process")
}

func (s *trackingSuite) TestCreateTransientScopeForTrackingUnhappyNoDBus(c *C) {
	// Pretend that refresh app awareness is enabled
	enableFeatures(c, features.RefreshAppAwareness)

	// Make it appear that DBus is entirely unavailable.
	noBus := func() (*dbus.Conn, error) {
		return nil, fmt.Errorf("dbus is not available for testing")
	}
	restore := dbusutil.MockConnections(noBus, noBus)
	defer restore()

	// Pretend we are a root user so that we attempt to use the system bus as fallback.
	restore = cgroup.MockOsGetuid(0)
	defer restore()
	// Pretend our PID is this value.
	restore = cgroup.MockOsGetpid(312123)
	defer restore()

	// Rig the random UUID generator to return this value.
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	restore = cgroup.MockRandomUUID(func() (string, error) {
		return uuid, nil
	})
	defer restore()

	// Calling StartTransientUnit is not attempted without a DBus connection.
	restore = cgroup.MockDoCreateTransientScope(func(conn *dbus.Conn, unitName string, pid int) error {
		c.Error("test sequence violated")
		return fmt.Errorf("test was not expected to create a transient scope")
	})
	defer restore()

	// Disable the cgroup analyzer function as we don't expect it to be used in this test.
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Error("test sequence violated")
		return "", fmt.Errorf("test was not expected to measure process path in the tracking cgroup")
	})
	defer restore()

	// Attempts to create a transient scope fail with a special error
	// indicating that we cannot track application process.
	err := cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, ErrorMatches, "cannot track application process")
}

func (s *trackingSuite) TestCreateTransientScopeForTrackingSilentlyFails(c *C) {
	// Pretend that refresh app awareness is enabled
	enableFeatures(c, features.RefreshAppAwareness)

	// Hand out stub connections to both the system and session bus.
	// Neither is really used here but they must appear to be available.
	restore := dbusutil.MockConnections(dbustest.StubConnection, dbustest.StubConnection)
	defer restore()

	// Pretend we are a non-root user.
	restore = cgroup.MockOsGetuid(12345)
	defer restore()
	// Pretend our PID is this value.
	restore = cgroup.MockOsGetpid(312123)
	defer restore()

	// Rig the random UUID generator to return this value.
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	restore = cgroup.MockRandomUUID(func() (string, error) {
		return uuid, nil
	})
	defer restore()

	// Calling StartTransientUnit succeeds but in reality does not move our
	// process to the new cgroup hierarchy. This can happen when systemd
	// version is < 238 and when the calling user is in a hierarchy that is
	// owned by another user. One example is a user logging in remotely over
	// ssh.
	restore = cgroup.MockDoCreateTransientScope(func(conn *dbus.Conn, unitName string, pid int) error {
		return nil
	})
	defer restore()

	// Rig the cgroup analyzer to pretend that we are not placed in a snap-related slice.
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Assert(pid, Equals, 312123)
		return "/system.slice/foo.service", nil
	})
	defer restore()

	// Attempts to create a transient scope fail with a special error
	// indicating that we cannot track application process even though
	// the DBus call has returned no error.
	err := cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, ErrorMatches, "cannot track application process")
}

func (s *trackingSuite) TestCreateTransientScopeForRootOnSystemBus(c *C) {
	// Pretend that refresh app awareness is enabled
	enableFeatures(c, features.RefreshAppAwareness)

	// Hand out stub connections to both the system and session bus. Remember
	// the identity of the system bus to that we can verify access later.
	// Neither is really used here but they must appear to be available.
	systemBus, err := dbustest.StubConnection()
	c.Assert(err, IsNil)
	restore := dbusutil.MockConnections(func() (*dbus.Conn, error) { return systemBus, nil }, dbustest.StubConnection)
	defer restore()

	// Pretend we are a root user. All hooks execute as root.
	restore = cgroup.MockOsGetuid(0)
	defer restore()

	// Pretend our PID is this value.
	restore = cgroup.MockOsGetpid(312123)
	defer restore()

	// Rig the random UUID generator to return this value.
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	restore = cgroup.MockRandomUUID(func() (string, error) {
		return uuid, nil
	})
	defer restore()

	// Pretend that attempting to create a transient scope succeeds.  Measure
	// the bus used and the unit name provided by the caller.  Note that the
	// call was made on the system bus, as requested by TrackingOptions below.
	restore = cgroup.MockDoCreateTransientScope(func(conn *dbus.Conn, unitName string, pid int) error {
		c.Assert(conn, Equals, systemBus)
		c.Assert(unitName, Equals, "snap.pkg.app-"+uuid+".scope")
		return nil
	})
	defer restore()

	// Rig the cgroup analyzer to indicate successful tracking.
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		return "snap.pkg.app-" + uuid + ".scope", nil
	})
	defer restore()

	// Create a transient scope and see it succeed.
	err = cgroup.CreateTransientScopeForTracking("snap.pkg.app", &cgroup.TrackingOptions{AllowSessionBus: false})
	c.Assert(err, IsNil)
}

type testTransientScopeConfirm struct {
	uuid        string
	securityTag string
	confirmUnit string
	confirmErr  error
	expectedErr string
}

func (s *trackingSuite) testCreateTransientScopeConfirm(c *C, tc testTransientScopeConfirm) {
	enableFeatures(c, features.RefreshAppAwareness)

	logBuf, restore := logger.MockLogger()
	defer restore()
	os.Setenv("SNAPD_DEBUG", "true")
	defer os.Unsetenv("SNAPD_DEBUG")
	restore = cgroup.MockOsGetuid(12345)
	defer restore()
	restore = cgroup.MockOsGetpid(312123)
	defer restore()
	restore = cgroup.MockRandomUUID(func() (string, error) {
		return tc.uuid, nil
	})
	defer restore()
	sessionBus, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		// we are not expecting any dbus traffic
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})

	c.Assert(err, IsNil)
	restore = dbusutil.MockOnlySessionBusAvailable(sessionBus)
	defer restore()
	restore = cgroup.MockDoCreateTransientScope(func(conn *dbus.Conn, unitName string, pid int) error {
		escapedTag, err := systemd.UnitNameFromSecurityTag(tc.securityTag)
		c.Assert(err, IsNil)

		c.Assert(conn, Equals, sessionBus)
		c.Assert(unitName, Equals, escapedTag+"-"+tc.uuid+".scope")
		return nil
	})
	defer restore()

	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		return tc.confirmUnit, tc.confirmErr
	})
	defer restore()

	// creating transient scope fails when systemd reports that the job failed
	err = cgroup.CreateTransientScopeForTracking(tc.securityTag, nil)
	if tc.expectedErr == "" {
		c.Assert(err, IsNil)
	} else {
		c.Assert(err, ErrorMatches, tc.expectedErr)
		if tc.confirmErr == nil {
			c.Check(logBuf.String(), testutil.Contains, "systemd could not associate process 312123 with transient scope")
		}
	}
}

func (s *trackingSuite) TestCreateTransientScopeConfirmHappy(c *C) {
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	s.testCreateTransientScopeConfirm(c, testTransientScopeConfirm{
		securityTag: "snap.pkg.app",
		uuid:        uuid,
		confirmUnit: "foo/bar/baz/snap.pkg.app-" + uuid + ".scope",
	})
}

func (s *trackingSuite) TestCreateTransientScopeConfirmEscaped(c *C) {
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	s.testCreateTransientScopeConfirm(c, testTransientScopeConfirm{
		securityTag: "snap.pkg+comp.hook.install",
		uuid:        uuid,
		confirmUnit: "foo/bar/baz/snap.pkg\\x2bcomp.hook.install-" + uuid + ".scope",
	})
}

func (s *trackingSuite) TestCreateTransientScopeConfirmOtherUnit(c *C) {
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	s.testCreateTransientScopeConfirm(c, testTransientScopeConfirm{
		securityTag: "snap.pkg.app",
		uuid:        uuid,
		confirmUnit: "foo/bar/baz/other-unit.scope",
		expectedErr: cgroup.ErrCannotTrackProcess.Error(),
	})
}

func (s *trackingSuite) TestCreateTransientScopeConfirmCheckError(c *C) {
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	s.testCreateTransientScopeConfirm(c, testTransientScopeConfirm{
		securityTag: "snap.pkg.app",
		uuid:        uuid,
		confirmErr:  fmt.Errorf("mock failure"),
		expectedErr: "mock failure",
	})
}

func (s *trackingSuite) TestCreateTransientScopeConfirmInvalidTag(c *C) {
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	s.testCreateTransientScopeConfirm(c, testTransientScopeConfirm{
		securityTag: "snap.pkg/comp.hook.install",
		uuid:        uuid,
		confirmErr:  fmt.Errorf("invalid character in security tag: '/'"),
		expectedErr: "invalid character in security tag: '/'",
	})
}

const systemdSignalMatch = `type='signal',interface='org.freedesktop.systemd1.Manager',member='JobRemoved'`

func checkSystemdSignalSubscribe(c *C, msg *dbus.Message) *dbus.Message {
	var rule string
	dbus.Store(msg.Body, &rule)
	c.Check(rule, Equals, systemdSignalMatch)
	return &dbus.Message{
		Type: dbus.TypeMethodReply,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
			dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
		},
	}
}

func checkSystemSignalUnsubscribe(c *C, msg *dbus.Message) *dbus.Message {
	var rule string
	dbus.Store(msg.Body, &rule)
	c.Check(rule, Equals, systemdSignalMatch)
	return &dbus.Message{
		Type: dbus.TypeMethodReply,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
			dbus.FieldSender:      dbus.MakeVariant(":1"), // This does not matter.
		},
	}
}

func checkAndRespondToStartTransientUnit(c *C, msg *dbus.Message, scopeName string, pid int) *dbus.Message {
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

func checkAndFailToStartTransientUnit(c *C, msg *dbus.Message, errMsg string) *dbus.Message {
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

func mockJobRemovedSignal(unit, result string) *dbus.Message {
	return &dbus.Message{
		Type: dbus.TypeSignal,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldSender:    dbus.MakeVariant(":1"), // This does not matter.
			dbus.FieldPath:      dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/systemd1")),
			dbus.FieldInterface: dbus.MakeVariant("org.freedesktop.systemd1.Manager"),
			dbus.FieldMember:    dbus.MakeVariant("JobRemoved"),
			dbus.FieldSignature: dbus.MakeVariant(
				dbus.SignatureOf(uint32(0), dbus.ObjectPath(""), "", "")),
		},
		Body: []interface{}{
			uint32(1),
			dbus.ObjectPath("/org/freedesktop/systemd1/job/1462"),
			unit,
			result,
		},
	}
}

func (s *trackingSuite) TestCreateTransientScopeHappyWithRetriedCheckCgroupV1(c *C) {
	enableFeatures(c, features.RefreshAppAwareness)

	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()
	logBuf, restore := logger.MockLogger()
	defer restore()
	os.Setenv("SNAPD_DEBUG", "true")
	defer os.Unsetenv("SNAPD_DEBUG")
	restore = cgroup.MockOsGetuid(12345)
	defer restore()
	restore = cgroup.MockOsGetpid(312123)
	defer restore()
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	restore = cgroup.MockRandomUUID(func() (string, error) {
		return uuid, nil
	})
	defer restore()
	sessionBus, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		c.Logf("%v got msg: %v", n, msg)
		switch n {
		case 0:
			return []*dbus.Message{checkAndRespondToStartTransientUnit(c, msg, "snap.pkg.app-"+uuid+".scope", 312123)}, nil
			// CreateTransientScopeForTracking is called twice in the test
		case 1:
			return []*dbus.Message{checkAndRespondToStartTransientUnit(c, msg, "snap.pkg.app-"+uuid+".scope", 312123)}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})

	c.Assert(err, IsNil)
	restore = dbusutil.MockOnlySessionBusAvailable(sessionBus)
	defer restore()

	pathInTrackingCgroupCallsToSuccess := 5
	pathInTrackingCgroupCalls := 0
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		pathInTrackingCgroupCalls++
		if pathInTrackingCgroupCalls < pathInTrackingCgroupCallsToSuccess {
			return "vte-spawn-1234-1234-1234.scope", nil
		}
		return "snap.pkg.app-" + uuid + ".scope", nil
	})
	defer restore()

	// creating transient scope succeeds even if check is retried
	err = cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, IsNil)
	c.Check(pathInTrackingCgroupCalls, Equals, 5)
	c.Check(logBuf.String(), Not(testutil.Contains), "systemd could not associate process 312123 with transient scope")

	// try again, but exhaust the limit if attempts which should be set to 100
	pathInTrackingCgroupCalls = 0
	pathInTrackingCgroupCallsToSuccess = 99999
	logBuf.Reset()
	err = cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, ErrorMatches, "cannot track application process")
	c.Check(pathInTrackingCgroupCalls, Equals, 100)
	c.Check(logBuf.String(), testutil.Contains, "systemd could not associate process 312123 with transient scope")
}

func (s *trackingSuite) TestCreateTransientScopeUnhappyJobFailed(c *C) {
	enableFeatures(c, features.RefreshAppAwareness)

	restore := cgroup.MockOsGetuid(12345)
	defer restore()
	restore = cgroup.MockOsGetpid(312123)
	defer restore()
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	restore = cgroup.MockRandomUUID(func() (string, error) {
		return uuid, nil
	})
	defer restore()
	signalChan := make(chan struct{})
	sessionBus, inject, err := dbustest.InjectableConnection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		c.Logf("dbus message %v", n)
		switch n {
		case 0:
			return []*dbus.Message{checkSystemdSignalSubscribe(c, msg)}, nil
		case 1:
			defer func() {
				close(signalChan)
			}()
			return []*dbus.Message{checkAndRespondToStartTransientUnit(c, msg, "snap.pkg.app-"+uuid+".scope", 312123)}, nil
		case 2:
			return []*dbus.Message{checkSystemSignalUnsubscribe(c, msg)}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	go func() {
		select {
		case <-signalChan:
			inject(mockJobRemovedSignal("snap.pkg.app-"+uuid+".scope", "failed"))
		}
	}()

	c.Assert(err, IsNil)
	restore = dbusutil.MockOnlySessionBusAvailable(sessionBus)
	defer restore()

	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Fatal("unexpected call")
		return "", fmt.Errorf("unexpected call")
	})
	defer restore()

	// creating transient scope fails when systemd reports that the job failed
	err = cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, ErrorMatches, "transient scope could not be started, job /org/freedesktop/systemd1/job/1462 finished with result failed")
}

func (s *trackingSuite) TestCreateTransientScopeUnhappyJobTimeout(c *C) {
	enableFeatures(c, features.RefreshAppAwareness)

	restore := cgroup.MockOsGetuid(12345)
	defer restore()
	restore = cgroup.MockOsGetpid(312123)
	defer restore()
	uuid := "cc98cd01-6a25-46bd-b71b-82069b71b770"
	restore = cgroup.MockRandomUUID(func() (string, error) {
		return uuid, nil
	})
	defer restore()
	restore = cgroup.MockCreateScopeJobTimeout(100 * time.Millisecond)
	defer restore()
	// mock a connection, but without support for injecting signal messages
	sessionBus, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		c.Logf("dbus message %v", n)
		switch n {
		case 0:
			return []*dbus.Message{checkSystemdSignalSubscribe(c, msg)}, nil
		case 1:
			return []*dbus.Message{checkAndRespondToStartTransientUnit(c, msg, "snap.pkg.app-"+uuid+".scope", 312123)}, nil
		case 2:
			return []*dbus.Message{checkSystemSignalUnsubscribe(c, msg)}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})

	c.Assert(err, IsNil)
	restore = dbusutil.MockOnlySessionBusAvailable(sessionBus)
	defer restore()

	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Fatal("unexpected call")
		return "", fmt.Errorf("unexpected call")
	})
	defer restore()

	// creating transient scope fails when systemd reports that the job failed
	err = cgroup.CreateTransientScopeForTracking("snap.pkg.app", nil)
	c.Assert(err, ErrorMatches, "transient scope not created in 100ms")
}

func (s *trackingSuite) TestDoCreateTransientScopeHappyCgroupV2(c *C) {
	signalChan := make(chan struct{})
	conn, inject, err := dbustest.InjectableConnection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		c.Logf("message: %v", msg)
		switch n {
		case 0:
			return []*dbus.Message{checkSystemdSignalSubscribe(c, msg)}, nil
		case 1:
			defer func() {
				close(signalChan)
			}()
			return []*dbus.Message{checkAndRespondToStartTransientUnit(c, msg, "foo.scope", 312123)}, nil
		case 2:
			return []*dbus.Message{checkSystemSignalUnsubscribe(c, msg)}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	go func() {
		select {
		case <-signalChan:
			inject(mockJobRemovedSignal("foo.scope", "done"))
		}
	}()

	c.Assert(err, IsNil)
	defer conn.Close()
	err = cgroup.DoCreateTransientScope(conn, "foo.scope", 312123)
	c.Assert(err, IsNil)
}

func (s *trackingSuite) TestDoCreateTransientScopeHappyCgroupV1(c *C) {
	restore := cgroup.MockVersion(cgroup.V1, nil)
	defer restore()

	conn, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		c.Logf("message: %v", msg)
		switch n {
		case 0:
			return []*dbus.Message{checkAndRespondToStartTransientUnit(c, msg, "foo.scope", 312123)}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	c.Assert(err, IsNil)
	defer conn.Close()
	err = cgroup.DoCreateTransientScope(conn, "foo.scope", 312123)
	c.Assert(err, IsNil)
}

func (s *trackingSuite) TestDoCreateTransientScopeForwardedErrors(c *C) {
	// Certain errors are forwarded and handled in the logic calling into
	// DoCreateTransientScope. Those are tested here.
	for _, t := range []struct {
		dbusError, msg string
	}{
		{"org.freedesktop.DBus.Error.NameHasNoOwner", "dbus name has no owner"},
		{"org.freedesktop.DBus.Error.UnknownMethod", "unknown dbus object method"},
		{"org.freedesktop.DBus.Error.Spawn.ChildExited", "dbus spawned child process exited"},
	} {
		conn, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
			switch n {
			case 0:
				return []*dbus.Message{checkSystemdSignalSubscribe(c, msg)}, nil
			case 1:
				return []*dbus.Message{checkAndFailToStartTransientUnit(c, msg, t.dbusError)}, nil
			case 2:
				return []*dbus.Message{checkSystemSignalUnsubscribe(c, msg)}, nil
			}
			return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
		})
		c.Assert(err, IsNil)
		defer conn.Close()
		err = cgroup.DoCreateTransientScope(conn, "foo.scope", 312123)
		c.Assert(strings.HasSuffix(err.Error(), fmt.Sprintf(" [%s]", t.dbusError)), Equals, true, Commentf("%q ~ %s", err, t.dbusError))
		c.Check(err, ErrorMatches, t.msg+" .*")
	}
}

func (s *trackingSuite) TestDoCreateTransientScopeClashingScopeName(c *C) {
	// In case our UUID algorithm is bad and systemd reports that an unit with
	// identical name already exists, we provide a special error handler for that.
	errMsg := "org.freedesktop.systemd1.UnitExists"
	conn, err := dbustest.Connection(func(msg *dbus.Message, n int) ([]*dbus.Message, error) {
		switch n {
		case 0:
			return []*dbus.Message{checkSystemdSignalSubscribe(c, msg)}, nil
		case 1:
			return []*dbus.Message{checkAndFailToStartTransientUnit(c, msg, errMsg)}, nil
		case 2:
			return []*dbus.Message{checkSystemSignalUnsubscribe(c, msg)}, nil
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
			return []*dbus.Message{checkSystemdSignalSubscribe(c, msg)}, nil
		case 1:
			return []*dbus.Message{checkAndFailToStartTransientUnit(c, msg, errMsg)}, nil
		case 2:
			return []*dbus.Message{checkSystemSignalUnsubscribe(c, msg)}, nil
		}
		return nil, fmt.Errorf("unexpected message #%d: %s", n, msg)
	})
	c.Assert(err, IsNil)
	defer conn.Close()
	err = cgroup.DoCreateTransientScope(conn, "foo.scope", 312123)
	c.Assert(err, ErrorMatches, `cannot create transient scope: DBus error "org.example.BadHairDay": \[\]`)
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
		return dbustest.StubConnection()
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
		return dbustest.StubConnection()
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

func (s *trackingSuite) TestConfirmSystemdServiceTrackingHappy(c *C) {
	// Pretend our PID is this value.
	restore := cgroup.MockOsGetpid(312123)
	defer restore()
	// Replace the cgroup analyzer function
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Assert(pid, Equals, 312123)
		return "/user.slice/user-12345.slice/user@12345.service/snap.pkg.app.service", nil
	})
	defer restore()

	// With the cgroup path faked as above, we are being tracked as the systemd
	// service so no error is reported.
	err := cgroup.ConfirmSystemdServiceTracking("snap.pkg.app")
	c.Assert(err, IsNil)
}

func (s *trackingSuite) TestConfirmSystemdServiceTrackingSad(c *C) {
	// Pretend our PID is this value.
	restore := cgroup.MockOsGetpid(312123)
	defer restore()
	// Replace the cgroup analyzer function
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Assert(pid, Equals, 312123)
		// Tracking path of a gnome terminal helper process. Meant to illustrate a tracking but not related to a snap application.
		return "user.slice/user-12345.slice/user@12345.service/apps.slice/apps-org.gnome.Terminal.slice/vte-spawn-e640104a-cddf-4bd8-ba4b-2c1baf0270c3.scope", nil
	})
	defer restore()

	// With the cgroup path faked as above, tracking is not effective.
	err := cgroup.ConfirmSystemdServiceTracking("snap.pkg.app")
	c.Assert(err, Equals, cgroup.ErrCannotTrackProcess)
}

func (s *trackingSuite) TestConfirmSystemdAppTrackingHappy(c *C) {
	// Pretend our PID is this value.
	restore := cgroup.MockOsGetpid(312123)
	defer restore()
	// Replace the cgroup analyzer function
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Assert(pid, Equals, 312123)
		return "user.slice/user-12345.slice/user@12345.service/apps.slice/snap.pkg.app-ae6d0825-dacd-454c-baec-67289f067c28.scope", nil
	})
	defer restore()

	// With the cgroup path faked as above, we are being tracked so no error is reported.
	err := cgroup.ConfirmSystemdAppTracking("snap.pkg.app")
	c.Assert(err, IsNil)
}

func (s *trackingSuite) TestConfirmSystemdAppTrackingEscaped(c *C) {
	// Pretend our PID is this value.
	restore := cgroup.MockOsGetpid(312123)
	defer restore()
	// Replace the cgroup analyzer function
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Assert(pid, Equals, 312123)
		return "user.slice/user-12345.slice/user@12345.service/apps.slice/snap.pkg\\x2bcomp.hook.install-ae6d0825-dacd-454c-baec-67289f067c28.scope", nil
	})
	defer restore()

	// With the cgroup path faked as above, we are being tracked so no error is reported.
	err := cgroup.ConfirmSystemdAppTracking("snap.pkg+comp.hook.install")
	c.Assert(err, IsNil)
}

func (s *trackingSuite) TestConfirmSystemdAppTrackingInvalidTag(c *C) {
	restore := cgroup.MockOsGetpid(312123)
	defer restore()

	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Fatalf("unexpected call")
		return "", nil
	})
	defer restore()

	err := cgroup.ConfirmSystemdAppTracking("snap.pkg/comp.hook.install")
	c.Assert(err, ErrorMatches, "invalid character in security tag: '/'")
}

func (s *trackingSuite) TestConfirmSystemdAppTrackingSad1(c *C) {
	// Pretend our PID is this value.
	restore := cgroup.MockOsGetpid(312123)
	defer restore()
	// Replace the cgroup analyzer function
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Assert(pid, Equals, 312123)
		// mark as being tracked as a service
		return "/user.slice/user-12345.slice/user@12345.service/snap.pkg.app.service", nil
	})
	defer restore()

	// With the cgroup path faked as above, tracking is not effective.
	err := cgroup.ConfirmSystemdAppTracking("snap.pkg.app")
	c.Assert(err, Equals, cgroup.ErrCannotTrackProcess)
}

func (s *trackingSuite) TestConfirmSystemdAppTrackingSad2(c *C) {
	// Pretend our PID is this value.
	restore := cgroup.MockOsGetpid(312123)
	defer restore()
	// Replace the cgroup analyzer function
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Assert(pid, Equals, 312123)
		// Tracking path of a gnome terminal helper process. Meant to illustrate a tracking but not related to a snap application.
		return "user.slice/user-12345.slice/user@12345.service/apps.slice/apps-org.gnome.Terminal.slice/vte-spawn-e640104a-cddf-4bd8-ba4b-2c1baf0270c3.scope", nil
	})
	defer restore()

	// With the cgroup path faked as above, tracking is not effective.
	err := cgroup.ConfirmSystemdAppTracking("snap.pkg.app")
	c.Assert(err, Equals, cgroup.ErrCannotTrackProcess)
}

func (s *trackingSuite) TestConfirmSystemdAppTrackingSad3(c *C) {
	// Pretend our PID is this value.
	restore := cgroup.MockOsGetpid(312123)
	defer restore()
	// Replace the cgroup analyzer function
	restore = cgroup.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Assert(pid, Equals, 312123)
		// bad security tag
		return "user.slice/user-12345.slice/user@12345.service/apps.slice/snap.pkg-ae6d0825-dacd-454c-baec-67289f067c28.scope", nil
	})
	defer restore()

	// With the cgroup path faked as above, tracking is not effective.
	err := cgroup.ConfirmSystemdAppTracking("snap.pkg.app")
	c.Assert(err, Equals, cgroup.ErrCannotTrackProcess)
}
