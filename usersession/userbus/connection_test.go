// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

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
 */

package userbus_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/user"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/usersession/userbus"
)

// Reexec the actual bridge code in a separate process. Only the binary path
// and test runtime root are substituted; credential selection and FD setup
// are performed by the production launcher.
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "userbus-parent-death" {
		runParentDeathHelper()
		os.Exit(0)
	}
	if len(os.Args) >= 4 && os.Args[1] == "snapd-userbus" {
		dirs.SetRootDir(os.Args[3])
		uid, err := strconv.ParseUint(os.Args[2], 10, 32)
		if err == nil {
			err = userbus.RunBridge(context.Background(), uint32(uid), os.NewFile(3, "userbus"))
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type connectionSuite struct {
	testutil.BaseTest
	root   string
	helper *exec.Cmd
}

var _ = Suite(&connectionSuite{})

func (s *connectionSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.root = c.MkDir()
	s.helper = nil
	dirs.SetRootDir(s.root)
	s.AddCleanup(func() { dirs.SetRootDir("") })
	s.AddCleanup(userbus.MockInternalLibExecDir(func() (string, error) { return "/unused", nil }))
	s.AddCleanup(userbus.MockCommandContext(func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		exe, err := os.Executable()
		c.Assert(err, IsNil)
		if os.Geteuid() == 0 && args[1] != "0" {
			// go test's build directory is not traversable by other UIDs.
			data, err := os.ReadFile(exe)
			c.Assert(err, IsNil)
			exe = filepath.Join(s.root, "helper")
			c.Assert(os.WriteFile(exe, data, 0755), IsNil)
		}
		s.helper = exec.CommandContext(ctx, exe, append(args, s.root)...)
		return s.helper
	}))
}

func (s *connectionSuite) connect(c *C, uid uint32) *userbus.Connection {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	s.AddCleanup(cancel)
	conn, err := userbus.Connect(ctx, uid)
	c.Assert(err, IsNil)
	s.AddCleanup(func() { c.Check(conn.Close(), IsNil) })
	return conn
}

func (s *connectionSuite) startBus(c *C, uid uint32) *dbus.Conn {
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		c.Skip("dbus-daemon is not installed")
	}
	// A root test can launch the bus and bridge as a different user. Make
	// only this test's temporary directories traversable by that user.
	c.Assert(os.Chmod(filepath.Dir(s.root), 0755), IsNil)
	c.Assert(os.Chmod(s.root, 0755), IsNil)
	runtimeDir := filepath.Join(dirs.XdgRuntimeDirBase, strconv.FormatUint(uint64(uid), 10))
	c.Assert(os.MkdirAll(runtimeDir, 0755), IsNil)
	path := filepath.Join(runtimeDir, "bus")
	config := filepath.Join(s.root, "bus.conf")
	c.Assert(os.WriteFile(config, fmt.Appendf(nil, `<busconfig>
<type>session</type><listen>unix:path=%s</listen><auth>EXTERNAL</auth>
<policy context="default"><allow user="*"/><allow own="*"/>
<allow send_destination="*"/><allow receive_sender="*"/></policy>
</busconfig>`, path), 0644), IsNil)
	cmd := exec.Command("dbus-daemon", "--nofork", "--print-address", "--config-file="+config)
	if os.Geteuid() == 0 {
		account, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
		c.Assert(err, IsNil)
		gid, err := strconv.Atoi(account.Gid)
		c.Assert(err, IsNil)
		c.Assert(os.Chown(runtimeDir, int(uid), gid), IsNil)
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uint32(gid)}}
	}
	cmd.Stderr = os.Stderr
	output, err := cmd.StdoutPipe()
	c.Assert(err, IsNil)
	c.Assert(cmd.Start(), IsNil)
	s.AddCleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
	})
	scanner := bufio.NewScanner(output)
	c.Assert(scanner.Scan(), Equals, true)
	c.Assert(scanner.Err(), IsNil)
	server, err := dbus.Connect(scanner.Text())
	c.Assert(err, IsNil)
	s.AddCleanup(func() { server.Close() })
	return server
}

func (s *connectionSuite) checkIdentity(c *C, conn *userbus.Connection, uid uint32) {
	var busUID, pid uint32
	c.Assert(conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixUser", 0, conn.Names()[0]).Store(&busUID), IsNil)
	c.Check(busUID, Equals, uid)
	c.Assert(conn.BusObject().Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, conn.Names()[0]).Store(&pid), IsNil)
	c.Check(pid, Equals, uint32(s.helper.Process.Pid))
	c.Check(pid, Not(Equals), uint32(os.Getpid()))
	c.Check(conn.SupportsUnixFDs(), Equals, true)
}

func (s *connectionSuite) TestIdentityAndClose(c *C) {
	uid := uint32(os.Getuid())
	s.startBus(c, uid)
	conn := s.connect(c, uid)
	s.checkIdentity(c, conn, uid)
	c.Check(conn.Close(), IsNil)
	c.Check(conn.Close(), IsNil)
	c.Check(conn.Connected(), Equals, false)
	c.Check(s.helper.ProcessState, NotNil)
}

func (s *connectionSuite) TestDifferentUID(c *C) {
	if os.Geteuid() != 0 {
		c.Skip("requires root in an isolated test environment")
	}
	account, err := user.Lookup("nobody")
	c.Assert(err, IsNil)
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	c.Assert(err, IsNil)
	s.startBus(c, uint32(uid))
	conn := s.connect(c, uint32(uid))
	s.checkIdentity(c, conn, uint32(uid))
}

func (s *connectionSuite) TestMissingBus(c *C) {
	conn, err := userbus.Connect(context.Background(), uint32(os.Getuid()))
	c.Assert(conn, IsNil)
	c.Assert(err, ErrorMatches, `cannot connect to user [0-9]+ bus: cannot connect to user bus: .*no such file or directory.*`)
	c.Check(s.helper.ProcessState, NotNil)
}

func (s *connectionSuite) TestCancelAuthentication(c *C) {
	uid := uint32(os.Getuid())
	path := filepath.Join(dirs.XdgRuntimeDirBase, strconv.FormatUint(uint64(uid), 10), "bus")
	c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
	listener, err := net.Listen("unix", path)
	c.Assert(err, IsNil)
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		conn, err := userbus.Connect(ctx, uid)
		if conn != nil {
			conn.Close()
		}
		result <- err
	}()
	select {
	case peer := <-accepted:
		c.Assert(peer, NotNil)
		defer peer.Close()
		c.Assert(peer.SetReadDeadline(time.Now().Add(5*time.Second)), IsNil)
		var auth [6]byte
		_, err := io.ReadFull(peer, auth[:])
		c.Assert(err, IsNil)
		cancel()
	case <-time.After(5 * time.Second):
		c.Fatal("bridge did not connect")
	}
	select {
	case err := <-result:
		c.Check(errors.Is(err, context.Canceled), Equals, true)
	case <-time.After(5 * time.Second):
		c.Fatal("authentication did not unblock")
	}
	c.Check(s.helper.ProcessState, NotNil)
}

type blockingService struct {
	entered chan struct{}
	release chan struct{}
}

func (s *blockingService) Wait() *dbus.Error {
	close(s.entered)
	<-s.release
	return nil
}

func (s *connectionSuite) TestMethodCancellationKeepsConnection(c *C) {
	uid := uint32(os.Getuid())
	server := s.startBus(c, uid)
	service := &blockingService{make(chan struct{}), make(chan struct{})}
	defer close(service.release)
	c.Assert(server.Export(service, "/test", "io.snapcraft.Test"), IsNil)
	conn := s.connect(c, uid)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	call := conn.Object(server.Names()[0], "/test").GoWithContext(ctx, "io.snapcraft.Test.Wait", 0, nil)
	select {
	case <-service.entered:
	case <-time.After(5 * time.Second):
		c.Fatal("method was not delivered")
	}
	cancel()
	select {
	case result := <-call.Done:
		c.Check(errors.Is(result.Err, context.Canceled), Equals, true)
	case <-time.After(5 * time.Second):
		c.Fatal("method did not cancel")
	}
	s.checkIdentity(c, conn, uid)
}

func (s *connectionSuite) TestCancelConnection(c *C) {
	uid := uint32(os.Getuid())
	s.startBus(c, uid)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, err := userbus.Connect(ctx, uid)
	c.Assert(err, IsNil)
	defer conn.Close()
	cancel()
	select {
	case <-userbus.BridgeDone(conn):
	case <-time.After(5 * time.Second):
		c.Fatal("bridge was not reaped")
	}
	select {
	case <-conn.Context().Done():
	case <-time.After(5 * time.Second):
		c.Fatal("D-Bus connection was not closed")
	}
}

func (s *connectionSuite) TestHelperDeath(c *C) {
	uid := uint32(os.Getuid())
	s.startBus(c, uid)
	conn := s.connect(c, uid)
	c.Assert(s.helper.Process.Kill(), IsNil)
	select {
	case <-conn.Context().Done():
	case <-time.After(5 * time.Second):
		c.Fatal("D-Bus connection did not observe helper exit")
	}
	c.Check(conn.Close(), IsNil)
	c.Check(s.helper.ProcessState, NotNil)
}

type fdService struct{ reply dbus.UnixFD }

func (s *fdService) Copy(in, out, diagnostic dbus.UnixFD) (dbus.UnixFD, *dbus.Error) {
	input := os.NewFile(uintptr(in), "input")
	output := os.NewFile(uintptr(out), "output")
	stderr := os.NewFile(uintptr(diagnostic), "stderr")
	defer input.Close()
	defer output.Close()
	defer stderr.Close()
	if _, err := io.Copy(output, input); err != nil {
		return 0, dbus.MakeFailedError(err)
	}
	if _, err := stderr.WriteString("separate stderr"); err != nil {
		return 0, dbus.MakeFailedError(err)
	}
	return s.reply, nil
}

func (s *fdService) Reject() *dbus.Error {
	return dbus.NewError("io.snapcraft.Test.Rejected", []any{"rejected", uint32(42)})
}

func (s *connectionSuite) TestFileDescriptorsAndErrors(c *C) {
	uid := uint32(os.Getuid())
	server := s.startBus(c, uid)
	reply, err := os.CreateTemp(s.root, "reply")
	c.Assert(err, IsNil)
	defer reply.Close()
	_, err = reply.WriteString("returned descriptor")
	c.Assert(err, IsNil)
	_, err = reply.Seek(0, io.SeekStart)
	c.Assert(err, IsNil)
	c.Assert(server.Export(&fdService{dbus.UnixFD(reply.Fd())}, "/test", "io.snapcraft.Test"), IsNil)
	conn := s.connect(c, uid)
	input, err := os.CreateTemp(s.root, "input")
	c.Assert(err, IsNil)
	defer input.Close()
	payload := bytes.Repeat([]byte("input data\n"), 20000)
	_, err = input.Write(payload)
	c.Assert(err, IsNil)
	_, err = input.Seek(0, io.SeekStart)
	c.Assert(err, IsNil)
	outR, outW, err := os.Pipe()
	c.Assert(err, IsNil)
	defer outR.Close()
	defer outW.Close()
	errR, errW, err := os.Pipe()
	c.Assert(err, IsNil)
	defer errR.Close()
	defer errW.Close()
	// Drain concurrently: the payload exceeds both pipe and relay buffers.
	output := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(outR); output <- data }()
	var returned dbus.UnixFD
	obj := conn.Object(server.Names()[0], "/test")
	c.Assert(obj.Call("io.snapcraft.Test.Copy", 0, dbus.UnixFD(input.Fd()), dbus.UnixFD(outW.Fd()), dbus.UnixFD(errW.Fd())).Store(&returned), IsNil)
	for _, file := range []*os.File{input, outW, errW} {
		_, err := file.Stat()
		c.Check(err, IsNil) // the caller still owns all three original FDs
	}
	outW.Close()
	errW.Close()
	c.Check(<-output, DeepEquals, payload)
	diagnostic, err := io.ReadAll(errR)
	c.Assert(err, IsNil)
	c.Check(string(diagnostic), Equals, "separate stderr")
	returnedFile := os.NewFile(uintptr(returned), "returned")
	defer returnedFile.Close()
	data, err := io.ReadAll(returnedFile)
	c.Assert(err, IsNil)
	c.Check(string(data), Equals, "returned descriptor")
	err = obj.Call("io.snapcraft.Test.Reject", 0).Err
	c.Check(err, DeepEquals, dbus.Error{Name: "io.snapcraft.Test.Rejected", Body: []any{"rejected", uint32(42)}})
}

func (s *connectionSuite) TestSignals(c *C) {
	uid := uint32(os.Getuid())
	server := s.startBus(c, uid)
	conn := s.connect(c, uid)
	signals := make(chan *dbus.Signal, 1)
	conn.Signal(signals)
	c.Assert(conn.AddMatchSignal(dbus.WithMatchSender(server.Names()[0]), dbus.WithMatchInterface("org.freedesktop.systemd1.Manager"), dbus.WithMatchMember("JobRemoved")), IsNil)
	path := dbus.ObjectPath("/org/freedesktop/systemd1/job/42")
	c.Assert(server.Emit("/org/freedesktop/systemd1", "org.freedesktop.systemd1.Manager.JobRemoved", uint32(42), path, "test.service", "done"), IsNil)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case signal := <-signals:
			if signal.Name == "org.freedesktop.systemd1.Manager.JobRemoved" {
				c.Check(signal.Body, DeepEquals, []any{uint32(42), path, "test.service", "done"})
				return
			}
		case <-timer.C:
			c.Fatal("JobRemoved did not cross the bridge")
		}
	}
}

func (s *connectionSuite) TestCredentialSelection(c *C) {
	s.AddCleanup(userbus.MockGeteuid(0))
	s.AddCleanup(userbus.MockLookupUser(func(uid string) (*user.User, error) {
		c.Check(uid, Equals, "1234")
		return &user.User{Uid: uid, Gid: "5678"}, nil
	}))
	s.AddCleanup(userbus.MockCommandContext(exec.CommandContext))
	s.AddCleanup(userbus.MockInternalLibExecDir(func() (string, error) { return "/snap/snapd/42/usr/lib/snapd", nil }))
	cmd, err := userbus.HelperCommand(context.Background(), 1234)
	c.Assert(err, IsNil)
	c.Check(cmd.Path, Equals, "/snap/snapd/42/usr/lib/snapd/snapd")
	c.Check(cmd.Args, DeepEquals, []string{"snapd", "snapd-userbus", "1234"})
	c.Check(cmd.Dir, Equals, "/")
	c.Check(cmd.Env, DeepEquals, []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "SNAP_REEXEC=0"})
	c.Check(cmd.SysProcAttr.Credential, DeepEquals, &syscall.Credential{Uid: 1234, Gid: 5678, Groups: []uint32{}})
	c.Check(cmd.SysProcAttr.Pdeathsig, Equals, syscall.SIGKILL)
}

func (s *connectionSuite) TestInvalidIdentity(c *C) {
	s.AddCleanup(userbus.MockGeteuid(1234))
	_, err := userbus.HelperCommand(context.Background(), 5678)
	c.Check(err, ErrorMatches, ".*without root privileges")
	_, err = userbus.HelperCommand(context.Background(), ^uint32(0))
	c.Check(err, ErrorMatches, "invalid user id")
	s.AddCleanup(userbus.MockLookupUser(func(string) (*user.User, error) {
		return &user.User{Gid: "4294967295"}, nil
	}))
	_, err = userbus.HelperCommand(context.Background(), 1234)
	c.Check(err, ErrorMatches, "invalid primary group for user 1234")
}
