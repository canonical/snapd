// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2024 Canonical Ltd
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

package daemon

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	sys "syscall"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/testutil"
)

type ucrednetSuite struct {
	testutil.BaseTest

	ucred *sys.Ucred
	err   error
}

var _ = check.Suite(&ucrednetSuite{})

func (s *ucrednetSuite) getUcred(fd, level, opt int) (*sys.Ucred, error) {
	return s.ucred, s.err
}

func (s *ucrednetSuite) SetUpSuite(c *check.C) {
	getUcred = s.getUcred
}

func (s *ucrednetSuite) SetUpTest(c *check.C) {
	s.BaseTest.SetUpTest(c)
	s.AddCleanup(apparmor.MockFeatures([]string{"mocked-kernel-feature"}, nil, nil, nil))
	s.AddCleanup(MockAppArmorLabelFromPid(func(int) (string, error) {
		return "unconfined", nil
	}))
	s.AddCleanup(MockCgroupProcessPathInTrackingCgroup(func(int) (string, error) {
		return "/", nil
	}))
}

func (s *ucrednetSuite) TearDownTest(c *check.C) {
	s.ucred = nil
	s.err = nil
	s.BaseTest.TearDownTest(c)
}
func (s *ucrednetSuite) TearDownSuite(c *check.C) {
	getUcred = sys.GetsockoptUcred
}

func (s *ucrednetSuite) TestAcceptConnContext(c *check.C) {
	readlinkCalls := 0
	restore := MockOsReadlink(func(path string) (string, error) {
		readlinkCalls++
		c.Check(path, check.Equals, "/proc/100/exe")
		return "/usr/bin/snap", nil
	})
	defer restore()

	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	wl := &ucrednetListener{Listener: l}

	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, check.IsNil)
	defer conn.Close()
	c.Check(readlinkCalls, check.Equals, 1)

	ctx := ucrednetConnContext(context.Background(), conn)
	u, err := ucrednetGet(ctx)
	c.Assert(err, check.IsNil)
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.PIDForPolkit, check.Equals, int32(100))
	name, err := u.UntrustedProcessExeName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "/usr/bin/snap")
	c.Check(readlinkCalls, check.Equals, 1)
	c.Check(conn.RemoteAddr().String(), check.Equals, conn.(*ucrednetConn).Conn.RemoteAddr().String())
}

func (s *ucrednetSuite) TestAcceptConnContextUnreadableExe(c *check.C) {
	lookupErr := errors.New("cannot read executable")
	restore := MockOsReadlink(func(path string) (string, error) {
		c.Check(path, check.Equals, "/proc/100/exe")
		return "", lookupErr
	})
	defer restore()

	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	sock := filepath.Join(c.MkDir(), "sock")
	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	wl := &ucrednetListener{Listener: l}
	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, check.IsNil)
	defer conn.Close()

	ctx := ucrednetConnContext(context.Background(), conn)
	u, err := ucrednetGet(ctx)
	c.Assert(err, check.IsNil)
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.PIDForPolkit, check.Equals, int32(100))
	name, err := u.UntrustedProcessExeName()
	c.Check(err, check.Equals, lookupErr)
	c.Check(name, check.Equals, "")
}

func (s *ucrednetSuite) TestAcceptConnContextInstanceNameFromCgroup(c *check.C) {
	lookupCalls := 0
	restore := MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		lookupCalls++
		c.Check(pid, check.Equals, 100)
		return "/system.slice/snap.some-snap_instance.app.service", nil
	})
	defer restore()

	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	sock := filepath.Join(c.MkDir(), "sock")
	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	wl := &ucrednetListener{Listener: l}
	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, check.IsNil)
	defer conn.Close()
	c.Check(lookupCalls, check.Equals, 1)

	ctx := ucrednetConnContext(context.Background(), conn)
	u, err := ucrednetGet(ctx)
	c.Assert(err, check.IsNil)
	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap_instance")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, sock)
	c.Check(u.PIDForPolkit, check.Equals, int32(100))
	c.Check(lookupCalls, check.Equals, 1)
}

func (s *ucrednetSuite) TestAcceptConnContextNoSecurityTag(c *check.C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	sock := filepath.Join(c.MkDir(), "sock")
	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	wl := &ucrednetListener{Listener: l}
	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, check.IsNil)
	defer conn.Close()

	ctx := ucrednetConnContext(context.Background(), conn)

	u, err := ucrednetGet(ctx)
	c.Assert(err, check.IsNil)

	_, err = u.SecurityTag()
	c.Check(err, check.ErrorMatches, "apparmor: invalid security tag\ncgroup: cannot find snap security tag")

	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, sock)
	c.Check(u.PIDForPolkit, check.Equals, int32(100))
}

func (s *ucrednetSuite) acceptConnContext(c *check.C) *ucrednet {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	sock := filepath.Join(c.MkDir(), "sock")
	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	wl := &ucrednetListener{Listener: l}
	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, check.IsNil)
	defer conn.Close()

	ctx := ucrednetConnContext(context.Background(), conn)
	u, err := ucrednetGet(ctx)
	c.Assert(err, check.IsNil)
	return u
}

func (s *ucrednetSuite) TestAcceptConnContextAppArmor(c *check.C) {
	s.AddCleanup(MockCgroupProcessPathInTrackingCgroup(func(int) (string, error) {
		c.Error("cgroups must not be queried when AppArmor identifies the snap")
		return "", nil
	}))

	calls := 0
	s.AddCleanup(MockAppArmorLabelFromPid(func(pid int) (string, error) {
		c.Check(pid, check.Equals, 100)
		calls++
		return "snap.some-snap.app", nil
	}))

	u := s.acceptConnContext(c)

	tag, err := u.SecurityTag()
	c.Assert(err, check.IsNil)
	c.Check(tag.String(), check.Equals, "snap.some-snap.app")

	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap")
	c.Check(calls, check.Equals, 1)
}

func (s *ucrednetSuite) TestAcceptConnContextAppArmorKernelProbeError(c *check.C) {
	s.AddCleanup(apparmor.MockFeatures(nil, errors.New("cannot probe kernel features"), nil, nil))
	s.AddCleanup(MockAppArmorLabelFromPid(func(int) (string, error) {
		c.Error("process labels must not be read when the AppArmor kernel probe fails")
		return "", nil
	}))

	calls := 0
	s.AddCleanup(MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Check(pid, check.Equals, 100)
		calls++
		return "/system.slice/snap.fallback-snap.app.service", nil
	}))

	u := s.acceptConnContext(c)
	c.Check(calls, check.Equals, 1)
	tag, err := u.SecurityTag()
	c.Assert(err, check.IsNil)
	c.Check(tag.String(), check.Equals, "snap.fallback-snap.app")
	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "fallback-snap")
}

func (s *ucrednetSuite) TestAcceptConnContextCgroupFallback(c *check.C) {

	for _, t := range []struct {
		label        string
		readLabelErr error
	}{
		{label: "unconfined"},
		{label: "snap.some-snap.app.garbage"},
		{readLabelErr: errors.New("cannot read security label")},
	} {
		comment := check.Commentf("label: %q, label error: %v", t.label, t.readLabelErr)

		var calls []string
		s.AddCleanup(MockAppArmorLabelFromPid(func(pid int) (string, error) {
			c.Check(pid, check.Equals, 100)
			calls = append(calls, "apparmor")
			return t.label, t.readLabelErr
		}))
		s.AddCleanup(MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
			c.Check(pid, check.Equals, 100)
			calls = append(calls, "cgroup")
			return "/system.slice/snap.fallback-snap.app.service", nil
		}))

		u := s.acceptConnContext(c)

		tag, err := u.SecurityTag()
		c.Assert(err, check.IsNil, comment)
		c.Check(tag.String(), check.Equals, "snap.fallback-snap.app", comment)

		name, err := u.InstanceName()
		c.Check(err, check.IsNil, comment)
		c.Check(name, check.Equals, "fallback-snap", comment)
		c.Check(calls, check.DeepEquals, []string{"apparmor", "cgroup"}, comment)
	}
}

func (s *ucrednetSuite) TestAcceptConnContextSecurityTagLookupErrors(c *check.C) {
	var calls []string
	s.AddCleanup(MockAppArmorLabelFromPid(func(pid int) (string, error) {
		c.Check(pid, check.Equals, 100)
		calls = append(calls, "apparmor")
		return "", errors.New("cannot read security label")
	}))
	s.AddCleanup(MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		c.Check(pid, check.Equals, 100)
		calls = append(calls, "cgroup")
		return "", errors.New("cannot find tracking cgroup")
	}))

	u := s.acceptConnContext(c)

	_, err := u.SecurityTag()
	c.Check(err, check.ErrorMatches, "apparmor: cannot read security label\ncgroup: cannot find tracking cgroup")
	c.Check(calls, check.DeepEquals, []string{"apparmor", "cgroup"})
}

func (s *ucrednetSuite) TestEmptyNames(c *check.C) {
	u := NewUcrednet("", "", 42, "/run/snap.socket")
	name, err := u.InstanceName()
	c.Check(name, check.Equals, "")
	c.Check(err, check.ErrorMatches, "security tag is not available")
	name, err = u.UntrustedProcessExeName()
	c.Check(name, check.Equals, "")
	c.Check(err, check.ErrorMatches, "process executable name is not available")
	tag, err := u.SecurityTag()
	c.Check(tag, check.IsNil)
	c.Check(err, check.ErrorMatches, "security tag is not available")
}

func (s *ucrednetSuite) TestString(c *check.C) {
	var u *ucrednet
	c.Check(u.String(), check.Equals, "snap=;uid=;socket=;")
	u = NewUcrednet("snap.some-snap_instance.app", "", 42, "/run/snap.socket")
	u.PIDForPolkit = 100
	c.Check(u.String(), check.Equals, "snap=some-snap_instance;uid=42;socket=/run/snap.socket;")
	u = NewUcrednet("", "", 42, "/run/snap.socket")
	u.PIDForPolkit = 100
	c.Check(u.String(), check.Equals, "pid=100;uid=42;socket=/run/snap.socket;")
}

func (s *ucrednetSuite) TestNonUnix(c *check.C) {
	l, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, check.IsNil)

	wl := &ucrednetListener{Listener: l}
	defer wl.Close()

	addr := l.Addr().String()

	go func() {
		cli, err := net.Dial("tcp", addr)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, check.IsNil)
	defer conn.Close()

	ctx := ucrednetConnContext(context.Background(), conn)
	u, err := ucrednetGet(ctx)
	c.Check(u, check.IsNil)
	c.Check(err, check.Equals, errNoPeerCredentials)
}

func (s *ucrednetSuite) TestAcceptErrors(c *check.C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	c.Assert(l.Close(), check.IsNil)

	wl := &ucrednetListener{Listener: l}

	_, err = wl.Accept()
	c.Assert(err, check.NotNil)
}

func (s *ucrednetSuite) TestUcredErrors(c *check.C) {
	s.err = errors.New("oopsie")
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)

	wl := &ucrednetListener{Listener: l}
	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, check.IsNil)
		cli.Close()
	}()

	_, err = wl.Accept()
	c.Assert(err, check.Equals, s.err)
}

func (s *ucrednetSuite) TestIdempotentClose(c *check.C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, check.IsNil)
	wl := &ucrednetListener{Listener: l}

	c.Assert(wl.Close(), check.IsNil)
	c.Assert(wl.Close(), check.IsNil)
}

func (s *ucrednetSuite) TestGetNothing(c *check.C) {
	u, err := ucrednetGet(context.Background())
	c.Check(err, check.Equals, errNoPeerCredentials)
	c.Check(u, check.IsNil)
}

func (s *ucrednetSuite) TestGet(c *check.C) {
	original := NewUcrednet("snap.some-snap.app", "/usr/bin/snap", 42, "/run/snap.socket")
	ctx := ucrednetWithCredentials(context.Background(), original)

	u, err := ucrednetGet(ctx)
	c.Assert(err, check.IsNil)

	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap")

	tag, err := u.SecurityTag()
	c.Assert(err, check.IsNil)
	c.Check(tag.String(), check.Equals, "snap.some-snap.app")

	proc, err := u.UntrustedProcessExeName()
	c.Check(err, check.IsNil)
	c.Check(proc, check.Equals, "/usr/bin/snap")

	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
}

func (s *ucrednetSuite) TestGetWithInterface(c *check.C) {
	ctx := ucrednetWithCredentials(context.Background(), NewUcrednet("snap.some-snap.app", "", 42, "/run/snap.socket"))
	ctx = ucrednetAttachInterface(ctx, "snap-refresh-observe")
	u, ifaces, err := ucrednetGetWithInterfaces(ctx)
	c.Assert(err, check.IsNil)
	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.DeepEquals, []string{"snap-refresh-observe"})

	// interfaces are optional
	ctx = ucrednetWithCredentials(context.Background(), u)
	u, ifaces, err = ucrednetGetWithInterfaces(ctx)
	c.Assert(err, check.IsNil)
	name, err = u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.IsNil)
}

func (s *ucrednetSuite) TestAttachInterface(c *check.C) {
	ctx := ucrednetWithCredentials(context.Background(), NewUcrednet("snap.some-snap.app", "", 42, "/run/snap.socket"))
	ctx = ucrednetAttachInterface(ctx, "snap-refresh-observe")
	u, ifaces, err := ucrednetGetWithInterfaces(ctx)
	c.Assert(err, check.IsNil)
	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.DeepEquals, []string{"snap-refresh-observe"})
}

func (s *ucrednetSuite) TestAttachInterfaceRepeatedly(c *check.C) {
	ctx := ucrednetWithCredentials(context.Background(), NewUcrednet("snap.some-snap.app", "", 42, "/run/snap.socket"))
	for i := 0; i < 2; i++ {
		ctx = ucrednetAttachInterface(ctx, "snap-refresh-observe")
		u, ifaces, err := ucrednetGetWithInterfaces(ctx)
		c.Assert(err, check.IsNil)
		name, err := u.InstanceName()
		c.Check(err, check.IsNil)
		c.Check(name, check.Equals, "some-snap")
		c.Check(u.Uid, check.Equals, uint32(42))
		c.Check(u.Socket, check.Equals, "/run/snap.socket")
		c.Check(ifaces, check.DeepEquals, []string{"snap-refresh-observe"})
	}
}

func (s *ucrednetSuite) TestAttachInterfaceMultiple(c *check.C) {
	ctx := ucrednetWithCredentials(context.Background(), NewUcrednet("snap.some-snap.app", "", 42, "/run/snap.socket"))
	ctx = ucrednetAttachInterface(ctx, "snap-refresh-observe")
	ctx = ucrednetAttachInterface(ctx, "snap-interfaces-requests-control")
	ctx = ucrednetAttachInterface(ctx, "snap-refresh-observe")
	ctx = ucrednetAttachInterface(ctx, "foo")

	u, ifaces, err := ucrednetGetWithInterfaces(ctx)
	c.Assert(err, check.IsNil)
	name, err := u.InstanceName()
	c.Check(err, check.IsNil)
	c.Check(name, check.Equals, "some-snap")
	c.Check(u.Uid, check.Equals, uint32(42))
	c.Check(u.Socket, check.Equals, "/run/snap.socket")
	c.Check(ifaces, check.DeepEquals, []string{
		"snap-refresh-observe",
		"snap-interfaces-requests-control",
		"foo",
	})
}
