// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"net"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/systemd"
)

type sdNotifyTestSuite struct{}

var _ = Suite(&sdNotifyTestSuite{})

func (sd *sdNotifyTestSuite) TestSdNotifyMissingNotifyState(c *C) {
	c.Check(systemd.SdNotify(""), ErrorMatches, "cannot use empty notify state")
}

func (sd *sdNotifyTestSuite) TestSdNotifyWrongNotifySocket(c *C) {
	for _, t := range []struct {
		env    string
		errStr string
	}{
		{"", "cannot find NOTIFY_SOCKET environment"},
		{"xxx", `cannot use NOTIFY_SOCKET "xxx"`},
	} {
		os.Setenv("NOTIFY_SOCKET", t.env)
		defer os.Unsetenv("NOTIFY_SOCKET")

		c.Check(systemd.SdNotify("something"), ErrorMatches, t.errStr)
	}
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

		conn := mylog.Check2(net.ListenUnixgram("unixgram", &net.UnixAddr{
			Name: sockPath,
			Net:  "unixgram",
		}))

		defer conn.Close()

		ch := make(chan string)
		go func() {
			var buf [128]byte
			n := mylog.Check2(conn.Read(buf[:]))

			ch <- string(buf[:n])
		}()
		mylog.Check(systemd.SdNotify("something"))

		c.Check(<-ch, Equals, "something")
	}
}
