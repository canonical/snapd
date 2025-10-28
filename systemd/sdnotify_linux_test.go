// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	c.Check(systemd.SdNotifyWithFds("", 1, 2), ErrorMatches, "invalid empty notify state")
}

func (sd *sdNotifyTestSuite) TestSdNotifyWithFdsMissingFds(c *C) {
	c.Check(systemd.SdNotifyWithFds("some-state"), ErrorMatches, "at least one file descriptor is required")
}

func (sd *sdNotifyTestSuite) testSdNotifyWrongNotifySocket(c *C, withFds bool) {
	for _, t := range []struct {
		env    string
		errStr string
	}{
		{"", "cannot find NOTIFY_SOCKET environment variable"},
		{"xxx", `cannot use NOTIFY_SOCKET "xxx"`},
	} {
		os.Setenv("NOTIFY_SOCKET", t.env)
		defer os.Unsetenv("NOTIFY_SOCKET")

		if withFds {
			c.Check(systemd.SdNotifyWithFds("something", 1, 2), ErrorMatches, t.errStr)
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
	fakeEnv := map[string]string{}
	restore := systemd.MockOsGetenv(func(k string) string {
		return fakeEnv[k]
	})
	defer restore()

	for _, sockPath := range []string{
		filepath.Join(c.MkDir(), "socket"),
		"@socket",
	} {
		fakeEnv["NOTIFY_SOCKET"] = sockPath

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

func panicOnErr(err error) {
	if err != nil {
		panic(err)
	}
}

func (sd *sdNotifyTestSuite) TestSdNotifyWithFdsIntegration(c *C) {
	fakeEnv := map[string]string{}
	restore := systemd.MockOsGetenv(func(k string) string {
		return fakeEnv[k]
	})
	defer restore()

	for _, sockPath := range []string{
		filepath.Join(c.MkDir(), "socket"),
		"@socket",
	} {
		fakeEnv["NOTIFY_SOCKET"] = sockPath

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

		fd1, err := unix.Open(filepath.Join(tmpdir, "file-1"), unix.O_RDWR|unix.O_CREAT, 0644)
		c.Assert(err, IsNil)
		_, err = unix.Write(fd1, []byte("hello-1"))
		c.Assert(err, IsNil)

		fd2, err := unix.Open(filepath.Join(tmpdir, "file-2"), unix.O_RDWR|unix.O_CREAT, 0644)
		c.Assert(err, IsNil)
		_, err = unix.Write(fd2, []byte("hello-2"))
		c.Assert(err, IsNil)

		c.Check(filepath.Join(tmpdir, "file-1"), testutil.FileEquals, "hello-1")
		c.Check(filepath.Join(tmpdir, "file-2"), testutil.FileEquals, "hello-2")

		err = systemd.SdNotifyWithFds("something", fd1, fd2)
		c.Assert(err, IsNil)

		<-ch

		c.Check(sdState, Equals, "something")
		c.Check(filepath.Join(tmpdir, "file-1"), testutil.FileEquals, "hello-from-the-other-side-1")
		c.Check(filepath.Join(tmpdir, "file-2"), testutil.FileEquals, "hello-from-the-other-side-2")

		c.Check(creds.Pid, Equals, int32(os.Getpid()))
		c.Check(creds.Uid, Equals, uint32(os.Getuid()))
		c.Check(creds.Gid, Equals, uint32(os.Getgid()))

		unix.Close(fd1)
		unix.Close(fd2)
	}
}
