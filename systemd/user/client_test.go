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
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program. If not, see <http://www.gnu.org/licenses/>.
 */

package user_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dbusutil/dbustest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/systemd/user"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

const managerPath = dbus.ObjectPath("/org/freedesktop/systemd1")
const managerInterface = "org.freedesktop.systemd1.Manager"

type manager struct {
	mu     sync.Mutex
	conn   *dbus.Conn
	result string
	called chan string
	wait   chan struct{}
}

func (m *manager) Subscribe() *dbus.Error { return nil }
func (m *manager) StartUnit(unit, mode string) (dbus.ObjectPath, *dbus.Error) {
	m.mu.Lock()
	result, called, wait := m.result, m.called, m.wait
	m.mu.Unlock()
	if mode != "replace" {
		return "", dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", nil)
	}
	path := dbus.ObjectPath("/org/freedesktop/systemd1/job/42")
	if called != nil {
		called <- unit
	}
	if result != "" {
		// Deliberately deliver JobRemoved before the method reply.
		// A different path with the same numeric ID must not match, and the
		// canonical name in a completion need not equal the request alias.
		m.conn.Emit(managerPath, managerInterface+".JobRemoved", uint32(42), dbus.ObjectPath("/org/freedesktop/systemd1/job/99"), unit, "failed")
		m.conn.Emit(managerPath, managerInterface+".JobRemoved", uint32(42), path, "snap.test.real.service", result)
	}
	if wait != nil {
		<-wait
	}
	return path, nil
}
func (m *manager) LoadUnit(unit string) (dbus.ObjectPath, *dbus.Error) {
	if unit == "snap.test.missing.service" {
		return "", dbus.NewError("org.freedesktop.systemd1.NoSuchUnit", []any{"unit does not exist"})
	}
	return "/org/freedesktop/systemd1/unit/test", nil
}

func (m *manager) EnableUnitFiles(units []string, runtime, force bool) (bool, []user.UnitFileChange, *dbus.Error) {
	if runtime || force {
		return false, nil, dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", nil)
	}
	return true, []user.UnitFileChange{{Type: "symlink", Path: "/home/test/.config/systemd/user/default.target.wants/" + units[0], Source: "/etc/systemd/user/" + units[0]}}, nil
}

func (m *manager) DisableUnitFiles(units []string, runtime bool) ([]user.UnitFileChange, *dbus.Error) {
	if runtime {
		return nil, dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", nil)
	}
	return nil, nil
}

func (m *manager) Reload() *dbus.Error { return nil }
func (m *manager) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	return map[string]dbus.Variant{
		"Id":               dbus.MakeVariant("snap.test.real.service"),
		"Names":            dbus.MakeVariant([]string{"snap.test.real.service", "snap.test.alias.service"}),
		"ActiveState":      dbus.MakeVariant("reloading"),
		"UnitFileState":    dbus.MakeVariant("static"),
		"NeedDaemonReload": dbus.MakeVariant(true),
	}, nil
}
func (m *manager) Get(iface, key string) (dbus.Variant, *dbus.Error) {
	return dbus.MakeVariant("notify"), nil
}

type clientSuite struct {
	testutil.DBusTest
	testutil.BaseTest
	ctx     context.Context
	manager *manager
	client  *user.Client
	conn    *dbus.Conn
	signals *user.SignalHandler
}

var _ = Suite(&clientSuite{})

func (s *clientSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	s.ctx = ctx
	s.AddCleanup(cancel)
	s.manager = &manager{conn: s.SessionBus, result: "done"}
	c.Assert(s.SessionBus.Export(s.manager, managerPath, managerInterface), IsNil)
	c.Assert(s.SessionBus.Export(s.manager, "/org/freedesktop/systemd1/unit/test", "org.freedesktop.DBus.Properties"), IsNil)
	_, err := s.SessionBus.RequestName("org.freedesktop.systemd1", dbus.NameFlagDoNotQueue)
	c.Assert(err, IsNil)
	s.AddCleanup(func() { s.SessionBus.ReleaseName("org.freedesktop.systemd1") })
	signals := user.NewSignalHandler()
	conn, err := dbus.ConnectSessionBus(dbus.WithSignalHandler(signals))
	c.Assert(err, IsNil)
	s.conn, s.signals = conn, signals
	s.AddCleanup(func() { conn.Close() })
	s.client, err = user.New(ctx, conn, signals)
	c.Assert(err, IsNil)
	s.AddCleanup(s.client.Close)
}

func (s *clientSuite) TearDownTest(c *C) { s.BaseTest.TearDownTest(c) }

func (s *clientSuite) TestEarlyCompletion(c *C) {
	c.Assert(s.client.Start(s.ctx, "snap.test.service"), IsNil)
}

func (s *clientSuite) TestJobFailure(c *C) {
	s.manager.mu.Lock()
	s.manager.result = "dependency"
	s.manager.mu.Unlock()
	c.Assert(s.client.Start(s.ctx, "snap.test.service"), ErrorMatches, `cannot StartUnit "snap.test.service": job result dependency`)
}

func (s *clientSuite) TestSkippedJobSucceedsLikeSystemctl(c *C) {
	s.manager.mu.Lock()
	s.manager.result = "skipped"
	s.manager.mu.Unlock()
	c.Assert(s.client.Start(s.ctx, "snap.test.service"), IsNil)
}

func (s *clientSuite) TestCancellationDoesNotStopUnit(c *C) {
	s.manager.mu.Lock()
	s.manager.result = ""
	s.manager.called = make(chan string, 1)
	s.manager.mu.Unlock()
	ctx, cancel := context.WithCancel(s.ctx)
	done := make(chan error, 1)
	go func() { done <- s.client.Start(ctx, "snap.test.service") }()
	c.Assert(<-s.manager.called, Equals, "snap.test.service")
	cancel()
	c.Assert(errors.Is(<-done, context.Canceled), Equals, true)
	// The same connection remains usable for a subsequent request.
	_, err := s.client.Status(s.ctx, []string{"snap.test.alias.service"})
	c.Assert(err, IsNil)
}

func (s *clientSuite) TestOwnerLossFailsPendingJob(c *C) {
	s.manager.mu.Lock()
	s.manager.result = ""
	s.manager.called = make(chan string, 1)
	s.manager.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- s.client.Start(s.ctx, "snap.test.service") }()
	<-s.manager.called
	_, err := s.SessionBus.ReleaseName("org.freedesktop.systemd1")
	c.Assert(err, IsNil)
	c.Assert(<-done, ErrorMatches, `.*user manager owner changed`)
}

func (s *clientSuite) TestStatusAliasProjection(c *C) {
	sts, err := s.client.Status(s.ctx, []string{"snap.test.alias.service"})
	c.Assert(err, IsNil)
	c.Assert(sts, DeepEquals, []*systemd.UnitStatus{{Name: "snap.test.alias.service", Id: "snap.test.real.service", Names: []string{"snap.test.real.service", "snap.test.alias.service"}, Daemon: "notify", Active: true, Enabled: true, Installed: true, NeedDaemonReload: true}})
}

func (s *clientSuite) TestMissingUnitStatus(c *C) {
	sts, err := s.client.Status(s.ctx, []string{"snap.test.missing.service"})
	c.Assert(err, IsNil)
	c.Assert(sts, DeepEquals, []*systemd.UnitStatus{{Name: "snap.test.missing.service", Id: "snap.test.missing.service", Names: []string{"snap.test.missing.service"}}})
}

func (s *clientSuite) TestPersistentEnableDisableAndReload(c *C) {
	changes, err := s.client.Enable(s.ctx, []string{"snap.test.service"})
	c.Assert(err, IsNil)
	c.Assert(changes, DeepEquals, []user.UnitFileChange{{Type: "symlink", Path: "/home/test/.config/systemd/user/default.target.wants/snap.test.service", Source: "/etc/systemd/user/snap.test.service"}})
	c.Assert(s.client.Disable(s.ctx, []string{"snap.test.service"}), IsNil)
	c.Assert(s.client.Reload(s.ctx), IsNil)
}

func (s *clientSuite) TestDisconnectFailsPendingJob(c *C) {
	s.manager.mu.Lock()
	s.manager.result = ""
	s.manager.called = make(chan string, 1)
	s.manager.mu.Unlock()
	done := make(chan error, 1)
	go func() { done <- s.client.Start(s.ctx, "snap.test.service") }()
	<-s.manager.called
	c.Assert(s.conn.Close(), IsNil)
	c.Assert(<-done, ErrorMatches, `.*user manager connection closed`)
}

func (s *clientSuite) TestSignalOverflowFailsOperation(c *C) {
	blocked := make(chan *dbus.Signal)
	s.signals.AddSignal(blocked)
	defer s.signals.RemoveSignal(blocked)
	s.signals.DeliverSignal("", "", &dbus.Signal{})
	c.Assert(s.client.Start(s.ctx, "snap.test.service"), ErrorMatches, `.*user manager signal queue overflow`)
}

func (s *clientSuite) TestWaitForManagerAttachingToFreshBus(c *C) {
	ownerQueries := 0
	conn, err := dbustest.Connection(func(msg *dbus.Message, _ int) ([]*dbus.Message, error) {
		reply := &dbus.Message{Type: dbus.TypeMethodReply, Headers: map[dbus.HeaderField]dbus.Variant{dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial())}}
		if msg.Headers[dbus.FieldMember].Value() != "GetNameOwner" {
			return []*dbus.Message{reply}, nil
		}
		ownerQueries++
		if ownerQueries == 1 {
			reply.Type = dbus.TypeError
			reply.Headers[dbus.FieldErrorName] = dbus.MakeVariant("org.freedesktop.DBus.Error.NameHasNoOwner")
			reply.Headers[dbus.FieldSignature] = dbus.MakeVariant(dbus.SignatureOf(""))
			reply.Body = []any{"manager has not attached yet"}
			acquired := &dbus.Message{Type: dbus.TypeSignal, Headers: map[dbus.HeaderField]dbus.Variant{
				dbus.FieldSender:    dbus.MakeVariant("org.freedesktop.DBus"),
				dbus.FieldPath:      dbus.MakeVariant(dbus.ObjectPath("/org/freedesktop/DBus")),
				dbus.FieldInterface: dbus.MakeVariant("org.freedesktop.DBus"),
				dbus.FieldMember:    dbus.MakeVariant("NameOwnerChanged"),
				dbus.FieldSignature: dbus.MakeVariant(dbus.SignatureOf("", "", "")),
			}, Body: []any{"org.freedesktop.systemd1", "", ":1.42"}}
			return []*dbus.Message{reply, acquired}, nil
		}
		reply.Headers[dbus.FieldSignature] = dbus.MakeVariant(dbus.SignatureOf(""))
		reply.Body = []any{":1.42"}
		return []*dbus.Message{reply}, nil
	})
	c.Assert(err, IsNil)
	defer conn.Close()
	client, err := user.New(s.ctx, conn, nil)
	c.Assert(err, IsNil)
	defer client.Close()
	c.Assert(ownerQueries, Equals, 2)
}

func (s *clientSuite) TestMissingManagerWaitIsBounded(c *C) {
	_, err := s.SessionBus.ReleaseName("org.freedesktop.systemd1")
	c.Assert(err, IsNil)
	signals := user.NewSignalHandler()
	conn, err := dbus.ConnectSessionBus(dbus.WithSignalHandler(signals))
	c.Assert(err, IsNil)
	defer conn.Close()
	ctx, cancel := context.WithTimeout(s.ctx, 20*time.Millisecond)
	defer cancel()
	client, err := user.New(ctx, conn, signals)
	c.Assert(client, IsNil)
	c.Assert(errors.Is(err, context.DeadlineExceeded), Equals, true)
}
