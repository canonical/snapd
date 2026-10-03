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

package service_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/dbusutil/dbustest"
	"github.com/snapcore/snapd/systemd"
	systemduser "github.com/snapcore/snapd/systemd/user"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/service"
)

func Test(t *testing.T) { TestingT(t) }

type fakeManager struct {
	log    []string
	fail   map[string]error
	cancel context.CancelFunc
}

func (m *fakeManager) act(ctx context.Context, action string) error {
	m.log = append(m.log, action)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.fail[action]; err != nil {
		if m.cancel != nil {
			m.cancel()
		}
		return err
	}
	return nil
}
func (m *fakeManager) Start(ctx context.Context, unit string) error { return m.act(ctx, "start "+unit) }
func (m *fakeManager) Stop(ctx context.Context, unit string) error  { return m.act(ctx, "stop "+unit) }
func (m *fakeManager) ReloadOrRestart(ctx context.Context, unit string) error {
	return m.act(ctx, "reload-or-restart "+unit)
}
func (m *fakeManager) Enable(ctx context.Context, units []string) ([]systemduser.UnitFileChange, error) {
	return nil, m.act(ctx, "enable "+strings.Join(units, " "))
}
func (m *fakeManager) Disable(ctx context.Context, units []string) error {
	return m.act(ctx, "disable "+strings.Join(units, " "))
}
func (m *fakeManager) Reload(ctx context.Context) error { return m.act(ctx, "reload") }
func (m *fakeManager) Status(ctx context.Context, units []string) ([]*systemd.UnitStatus, error) {
	if err := m.act(ctx, "status "+strings.Join(units, " ")); err != nil {
		return nil, err
	}
	sts := make([]*systemd.UnitStatus, len(units))
	for i, unit := range units {
		sts[i] = &systemd.UnitStatus{Name: unit, Enabled: true, Installed: true}
	}
	return sts, nil
}

type serviceSuite struct{ testutil.BaseTest }

var _ = Suite(&serviceSuite{})

func (s *serviceSuite) TestSelectionPolicy(c *C) {
	s.AddCleanup(service.MockManagers(func(context.Context) ([]int, error) { return []int{2000, 1000, 2000}, nil }, nil))
	targets, err := service.Select(context.Background(), nil)
	c.Assert(err, IsNil)
	c.Assert(targets.UIDs(), DeepEquals, []int{1000, 2000})
	targets, err = service.Select(context.Background(), []int{})
	c.Assert(err, IsNil)
	c.Assert(targets.UIDs(), HasLen, 0)
	targets, err = service.Select(context.Background(), []int{3000})
	c.Assert(err, IsNil)
	c.Assert(targets.WithUID(context.Background(), 3000, func(service.Manager) error { c.Fatal("unavailable manager opened"); return nil }), ErrorMatches, "user manager for uid 3000 is unavailable")
	_, err = service.Select(context.Background(), []int{-1})
	c.Assert(err, ErrorMatches, "invalid user id -1")
}

func (s *serviceSuite) TestSnapshotAndPartialFailure(c *C) {
	discoveries := 0
	closed := make(chan int, 2)
	s.AddCleanup(service.MockManagers(func(context.Context) ([]int, error) {
		discoveries++
		return []int{1000, 2000}, nil
	}, func(_ context.Context, uid int) (service.Manager, func(), error) {
		if uid == 2000 {
			return nil, nil, fmt.Errorf("regular bus missing")
		}
		return &fakeManager{}, func() { closed <- uid }, nil
	}))
	targets, err := service.Select(context.Background(), nil)
	c.Assert(err, IsNil)
	r := targets.Do(context.Background(), func(_ int, m service.Manager) error {
		return service.Start(context.Background(), m, []string{"snap.test.service"}, false)
	})
	c.Assert(r.Targets, DeepEquals, []int{1000, 2000})
	c.Assert(r.Succeeded, DeepEquals, []int{1000})
	c.Assert(r.Err(), ErrorMatches, ".*uid 2000.*regular bus missing")
	c.Assert(<-closed, Equals, 1000)
	c.Assert(discoveries, Equals, 1)
}

func (s *serviceSuite) TestSharedGateIsCancelable(c *C) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.AddCleanup(service.MockManagers(func(context.Context) ([]int, error) { return []int{1000}, nil }, func(context.Context, int) (service.Manager, func(), error) { return &fakeManager{}, func() {}, nil }))
	first, err := service.Select(ctx, nil)
	c.Assert(err, IsNil)
	second, err := service.Select(ctx, nil)
	c.Assert(err, IsNil)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- first.WithUID(ctx, 1000, func(service.Manager) error { close(entered); <-release; return nil })
	}()
	<-entered
	canceled, stop := context.WithCancel(ctx)
	stop()
	err = second.WithUID(canceled, 1000, func(service.Manager) error { c.Error("operations for the same UID overlapped"); return nil })
	c.Assert(errors.Is(err, context.Canceled), Equals, true)
	close(release)
	c.Assert(<-done, IsNil)
	c.Assert(second.WithUID(ctx, 1000, func(service.Manager) error { return nil }), IsNil)
}

func (s *serviceSuite) TestStartRollbackSurvivesCancellation(c *C) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startErr := errors.New("start failure")
	m := &fakeManager{cancel: cancel, fail: map[string]error{"start snap.test.two.service": startErr}}
	err := service.Start(ctx, m, []string{"snap.test.one.service", "snap.test.two.service", "snap.test.three.service"}, true)
	c.Assert(errors.Is(err, startErr), Equals, true)
	c.Assert(errors.Is(err, context.Canceled), Equals, false)
	c.Assert(m.log, DeepEquals, []string{
		"enable snap.test.one.service snap.test.two.service snap.test.three.service", "reload",
		"start snap.test.one.service", "start snap.test.two.service", "stop snap.test.one.service",
		"disable snap.test.one.service snap.test.two.service snap.test.three.service", "reload",
	})
}

func (s *serviceSuite) TestStopAggregatesAndDoesNotDisableOnFailure(c *C) {
	a, b := errors.New("first stop"), errors.New("second stop")
	m := &fakeManager{fail: map[string]error{"stop snap.test.one.service": a, "stop snap.test.two.service": b}}
	err := service.Stop(context.Background(), m, []string{"snap.test.one.service", "snap.test.two.service"}, true)
	c.Assert(errors.Is(err, a), Equals, true)
	c.Assert(errors.Is(err, b), Equals, true)
	c.Assert(m.log, DeepEquals, []string{"stop snap.test.one.service", "stop snap.test.two.service"})
}

func (s *serviceSuite) TestRestartOrder(c *C) {
	m := &fakeManager{}
	c.Assert(service.Restart(context.Background(), m, []string{"snap.test.one.service", "snap.test.two.service"}, false), IsNil)
	c.Assert(m.log, DeepEquals, []string{"stop snap.test.one.service", "start snap.test.one.service", "stop snap.test.two.service", "start snap.test.two.service"})
}

func (s *serviceSuite) TestRejectNonSnapUnits(c *C) {
	m := &fakeManager{}
	c.Assert(service.Start(context.Background(), m, []string{"other.service"}, true), ErrorMatches, "cannot manage non-snap unit.*")
	c.Assert(m.log, HasLen, 0)
}

func (s *serviceSuite) TestDiscoverRunningManagersOverPublicBus(c *C) {
	conn, err := dbustest.Connection(func(msg *dbus.Message, _ int) ([]*dbus.Message, error) {
		if msg.Headers[dbus.FieldMember].Value() != "ListUnits" || msg.Headers[dbus.FieldInterface].Value() != "org.freedesktop.systemd1.Manager" {
			return nil, fmt.Errorf("unexpected call: %v", msg)
		}
		// No login/session or agent fixtures: an active lingering manager is
		// just as eligible as an active manager with a logged-in user.
		units := []service.ListedUnit{
			{Name: "user@2000.service", ActiveState: "active", Path: "/unit/one", JobPath: "/"},
			{Name: "user@1000.service", ActiveState: "active", Path: "/unit/two", JobPath: "/"},
			{Name: "user@3000.service", ActiveState: "inactive", Path: "/unit/three", JobPath: "/"},
			{Name: "user@bad.service", ActiveState: "active", Path: "/unit/four", JobPath: "/"},
			{Name: "other.service", ActiveState: "active", Path: "/unit/five", JobPath: "/"},
			{Name: "user@4294967295.service", ActiveState: "active", Path: "/unit/six", JobPath: "/"},
		}
		return []*dbus.Message{{Type: dbus.TypeMethodReply, Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldReplySerial: dbus.MakeVariant(msg.Serial()),
			dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf(units)),
		}, Body: []any{units}}}, nil
	})
	c.Assert(err, IsNil)
	s.AddCleanup(func() { conn.Close() })
	s.AddCleanup(dbusutil.MockOnlySystemBusAvailable(conn))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	targets, err := service.Select(ctx, nil)
	c.Assert(err, IsNil)
	c.Assert(targets.UIDs(), DeepEquals, []int{1000, 2000})
}
