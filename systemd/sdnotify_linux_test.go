// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

/*
 * Copyright (C) 2014-2026 Canonical Ltd
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

package systemd_test

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

type sdNotifyTestSuite struct{}

var _ = Suite(&sdNotifyTestSuite{})

func (sd *sdNotifyTestSuite) TestSdNotifyMissingNotifyState(c *C) {
	c.Check(systemd.SdNotify(""), ErrorMatches, "invalid empty notify state")
}

func (sd *sdNotifyTestSuite) TestSdNotifyWithFdsMissingNotifyState(c *C) {
	c.Check(systemd.SdNotifyWithFds(""), ErrorMatches, "invalid empty notify state")
}

func (sd *sdNotifyTestSuite) TestSdNotifyWithFdsMissingFds(c *C) {
	c.Check(systemd.SdNotifyWithFds("some-state"), ErrorMatches, "at least one file is required")
}

func (sd *sdNotifyTestSuite) testSdNotifyWrongNotifySocket(c *C, withFds bool) {
	for _, t := range []struct {
		env    string
		errStr string
	}{
		{"", "cannot find NOTIFY_SOCKET environment variable"},
		{"xxx", `cannot use NOTIFY_SOCKET "xxx"`},
	} {
		restore := systemd.MockNotifySocket(t.env)
		defer restore()
		systemd.ResetSdNotify()

		if withFds {
			f, err := os.OpenFile(filepath.Join(c.MkDir(), "test"), os.O_RDWR|os.O_CREATE, 0644)
			c.Assert(err, IsNil)
			c.Check(systemd.SdNotifyWithFds("something", f), ErrorMatches, t.errStr)
		} else {
			c.Check(systemd.SdNotify("something"), ErrorMatches, t.errStr)
		}
	}
}

func (sd *sdNotifyTestSuite) TestSdNotifyWrongNotifySocket(c *C) {
	const withFds = false
	sd.testSdNotifyWrongNotifySocket(c, withFds)
}

func (sd *sdNotifyTestSuite) TestSdNotifyWithFdsWrongNotifySocket(c *C) {
	const withFds = true
	sd.testSdNotifyWrongNotifySocket(c, withFds)
}

func (sd *sdNotifyTestSuite) TestSdNotifyIntegration(c *C) {
	for _, sockPath := range []string{
		filepath.Join(c.MkDir(), "socket"),
		"@socket",
	} {
		restore := systemd.MockNotifySocket(sockPath)
		defer restore()
		systemd.ResetSdNotify()

		conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{
			Name: sockPath,
			Net:  "unixgram",
		})
		c.Assert(err, IsNil)
		defer conn.Close()

		ch := make(chan string)
		go func() {
			var buf [128]byte
			n, err := conn.Read(buf[:])
			c.Assert(err, IsNil)
			ch <- string(buf[:n])
		}()

		err = systemd.SdNotify("something")
		c.Assert(err, IsNil)
		c.Check(<-ch, Equals, "something")
	}
}

func (sd *sdNotifyTestSuite) testSdNotifyReconnectsAfterWriteError(c *C, withFds bool) {
	notify := systemd.SdNotify
	if withFds {
		f, err := os.OpenFile(filepath.Join(c.MkDir(), "fd"), os.O_RDWR|os.O_CREATE, 0644)
		c.Assert(err, IsNil)
		defer f.Close()

		notify = func(state string) error {
			return systemd.SdNotifyWithFds(state, f)
		}
	}

	for _, sockPath := range []string{
		filepath.Join(c.MkDir(), "socket"),
		"@socket",
	} {
		restore := systemd.MockNotifySocket(sockPath)
		defer restore()
		systemd.ResetSdNotify()

		addr := &net.UnixAddr{
			Name: sockPath,
			Net:  "unixgram",
		}

		readOne := func(conn *net.UnixConn) string {
			var buf [128]byte
			n, err := conn.Read(buf[:])
			c.Assert(err, IsNil)
			return string(buf[:n])
		}

		// initial notification connects and caches the connection
		conn1, err := net.ListenUnixgram("unixgram", addr)
		c.Assert(err, IsNil)

		err = notify("first")
		c.Assert(err, IsNil)
		c.Check(readOne(conn1), Equals, "first")
		c.Check(systemd.SdNotifyConnCache(), NotNil)

		// closing the listener makes subsequent writes fail, which
		// should drop the cached connection
		conn1.Close()

		err = notify("second")
		c.Assert(err, ErrorMatches, ".*connection refused")
		c.Check(systemd.SdNotifyConnCache(), IsNil)

		// a new listener at the same address receives notifications
		// again, proving the next call reconnected
		if !strings.HasPrefix(sockPath, "@") {
			// closing the listener leaves the socket file behind
			c.Assert(os.Remove(sockPath), IsNil)
		}
		conn2, err := net.ListenUnixgram("unixgram", addr)
		c.Assert(err, IsNil)

		err = notify("third")
		c.Assert(err, IsNil)
		c.Check(readOne(conn2), Equals, "third")
		c.Check(systemd.SdNotifyConnCache(), NotNil)

		conn2.Close()
	}
}

func (sd *sdNotifyTestSuite) TestSdNotifyReconnectsAfterWriteError(c *C) {
	const withFds = false
	sd.testSdNotifyReconnectsAfterWriteError(c, withFds)
}

func (sd *sdNotifyTestSuite) TestSdNotifyWithFdsReconnectsAfterWriteError(c *C) {
	const withFds = true
	sd.testSdNotifyReconnectsAfterWriteError(c, withFds)
}

func panicOnErr(err error) {
	if err != nil {
		panic(err)
	}
}

func (sd *sdNotifyTestSuite) TestSdNotifyWithFdsIntegration(c *C) {
	for _, sockPath := range []string{
		filepath.Join(c.MkDir(), "socket"),
		"@socket",
	} {
		restore := systemd.MockNotifySocket(sockPath)
		defer restore()
		systemd.ResetSdNotify()

		tmpdir := c.MkDir()

		conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{
			Name: sockPath,
			Net:  "unixgram",
		})
		c.Assert(err, IsNil)
		defer conn.Close()

		ch := make(chan bool)

		var sdState string
		var creds *unix.Ucred
		go func() {
			rawConn, err := conn.SyscallConn()
			panicOnErr(err)

			err = rawConn.Control(func(socketFd uintptr) {
				// Allow reading creds
				err = unix.SetsockoptInt(int(socketFd), unix.SOL_SOCKET, unix.SO_PASSCRED, 1)
				panicOnErr(err)

				oob := make([]byte, 128)
				buf := make([]byte, 128)
				var n, oobn int
				for {
					n, oobn, _, _, err = unix.Recvmsg(int(socketFd), buf, oob, 0)
					if err == nil {
						break
					}
					if !errors.Is(err, unix.EAGAIN) {
						panicOnErr(err)
					}
					time.Sleep(100 * time.Millisecond)
				}
				sdState = string(buf[:n])
				msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
				panicOnErr(err)
				if len(msgs) != 2 {
					panic("expected len(msgs) == 2")
				}
				for _, msg := range msgs {
					switch msg.Header.Type {
					case unix.SCM_RIGHTS:
						msgfds, err := unix.ParseUnixRights(&msg)
						panicOnErr(err)
						if len(msgfds) != 2 {
							panic("expected len(msgfds) == 2")
						}
						_, err = unix.Seek(msgfds[0], 0, 0)
						panicOnErr(err)
						_, err = unix.Write(msgfds[0], []byte("hello-from-the-other-side-1"))
						panicOnErr(err)
						_, err = unix.Seek(msgfds[1], 0, 0)
						panicOnErr(err)
						_, err = unix.Write(msgfds[1], []byte("hello-from-the-other-side-2"))
						panicOnErr(err)
					case unix.SCM_CREDENTIALS:
						creds, err = unix.ParseUnixCredentials(&msg)
						panicOnErr(err)
					default:
						panic(fmt.Sprintf("Unknown control message type: %d", msg.Header.Type))
					}
				}
			})
			panicOnErr(err)

			// done
			ch <- true
		}()

		f1, err := os.OpenFile(filepath.Join(tmpdir, "file-1"), os.O_RDWR|os.O_CREATE, 0644)
		c.Assert(err, IsNil)
		defer f1.Close()
		_, err = f1.Write([]byte("hello-1"))
		c.Assert(err, IsNil)

		f2, err := os.OpenFile(filepath.Join(tmpdir, "file-2"), os.O_RDWR|os.O_CREATE, 0644)
		c.Assert(err, IsNil)
		defer f2.Close()
		_, err = f2.Write([]byte("hello-2"))
		c.Assert(err, IsNil)

		c.Check(filepath.Join(tmpdir, "file-1"), testutil.FileEquals, "hello-1")
		c.Check(filepath.Join(tmpdir, "file-2"), testutil.FileEquals, "hello-2")

		err = systemd.SdNotifyWithFds("something", f1, f2)
		c.Assert(err, IsNil)

		<-ch

		c.Check(sdState, Equals, "something")
		c.Check(filepath.Join(tmpdir, "file-1"), testutil.FileEquals, "hello-from-the-other-side-1")
		c.Check(filepath.Join(tmpdir, "file-2"), testutil.FileEquals, "hello-from-the-other-side-2")

		c.Check(creds.Pid, Equals, int32(os.Getpid()))
		c.Check(creds.Uid, Equals, uint32(os.Getuid()))
		c.Check(creds.Gid, Equals, uint32(os.Getgid()))
	}
}
