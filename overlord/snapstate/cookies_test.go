// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package snapstate

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type cookiesSuite struct {
	testutil.BaseTest
	st      *state.State
	snapmgr *SnapManager
}

var _ = Suite(&cookiesSuite{})

func (s *cookiesSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.st = state.New(nil)
	s.snapmgr, _ = Manager(s.st, state.NewTaskRunner(s.st))
}

func (s *cookiesSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func checkCookie(c *C, st *state.State, snapName string) {
	var cookies map[string]string
	var found int
	var cookieID string

	c.Assert(st.Get("snap-cookies", &cookies), IsNil)

	for cookie, snap := range cookies {
		if snap == snapName {
			found = found + 1
			cookieID = cookie
		}
	}
	c.Assert(found, Equals, 1)

	c.Assert(fmt.Sprintf("%s/snap.%s", dirs.SnapCookieDir, snapName), testutil.FileEquals, cookieID)
	cookieBytes, err := base64.RawURLEncoding.DecodeString(cookieID)
	c.Assert(err, IsNil)
	c.Assert(cookieBytes, HasLen, 39)
}

func (s *cookiesSuite) TestSyncCookies(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// verify that SyncCookies creates a cookie for a snap that's missing it and removes stale/invalid cookies
	s.st.Set("snaps", map[string]*json.RawMessage{
		"some-snap":  nil,
		"other-snap": nil})
	staleCookieFile := filepath.Join(dirs.SnapCookieDir, "snap.stale-cookie-snap")
	c.Assert(os.WriteFile(staleCookieFile, nil, 0644), IsNil)
	c.Assert(osutil.FileExists(staleCookieFile), Equals, true)

	// some-snap doesn't have cookie
	cookies := map[string]string{
		"123456": "other-snap",
		"809809": "other-snap",
		"999999": "unknown-snap",
		"199989": "unknown-snap",
	}
	s.st.Set("snap-cookies", cookies)

	for i := 0; i < 2; i++ {
		s.snapmgr.SyncCookies(s.st)

		c.Assert(osutil.FileExists(staleCookieFile), Equals, false)

		var newCookies map[string]string
		err := s.st.Get("snap-cookies", &newCookies)
		c.Assert(err, IsNil)
		c.Assert(newCookies, HasLen, 2)

		cookieFile := filepath.Join(dirs.SnapCookieDir, "snap.some-snap")
		c.Assert(osutil.FileExists(cookieFile), Equals, true)
		data, err := ioutil.ReadFile(cookieFile)
		c.Assert(err, IsNil)
		c.Assert(newCookies[string(data)], NotNil)
		c.Assert(newCookies[string(data)], Equals, "some-snap")

		cookieFile = filepath.Join(dirs.SnapCookieDir, "snap.other-snap")
		c.Assert(osutil.FileExists(cookieFile), Equals, true)
		data, err = ioutil.ReadFile(cookieFile)
		c.Assert(err, IsNil)
		c.Assert(newCookies[string(data)], NotNil)
		c.Assert(newCookies[string(data)], Equals, "other-snap")
	}
}

func (s *cookiesSuite) TestCreateSnapCookie(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	c.Assert(s.snapmgr.createSnapCookie(s.st, "foo"), IsNil)
	checkCookie(c, s.st, "foo")
	c.Assert(s.snapmgr.createSnapCookie(s.st, "foo"), IsNil)
	checkCookie(c, s.st, "foo")
}

func (s *cookiesSuite) TestRemoveSnapCookie(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	cookieFile := filepath.Join(dirs.SnapCookieDir, "snap.bar")

	c.Assert(os.WriteFile(cookieFile, nil, 0644), IsNil)

	// remove should not fail if cookie is not there
	c.Assert(s.snapmgr.removeSnapCookie(s.st, "bar"), IsNil)
	c.Assert(osutil.FileExists(cookieFile), Equals, false)

	c.Assert(s.snapmgr.createSnapCookie(s.st, "foo"), IsNil)
	c.Assert(s.snapmgr.createSnapCookie(s.st, "bar"), IsNil)
	c.Assert(osutil.FileExists(cookieFile), Equals, true)

	c.Assert(s.snapmgr.removeSnapCookie(s.st, "bar"), IsNil)
	c.Assert(osutil.FileExists(cookieFile), Equals, false)

	var cookies map[string]string
	c.Assert(s.st.Get("snap-cookies", &cookies), IsNil)
	c.Assert(cookies, HasLen, 1)

	// cookie for snap "foo" remains untouched
	checkCookie(c, s.st, "foo")
}
