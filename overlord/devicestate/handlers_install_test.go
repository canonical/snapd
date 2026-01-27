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
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package devicestate_test

import (
	"errors"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snapdtool"
	"github.com/snapcore/snapd/testutil"
)

type handlersInstallSuite struct {
	deviceMgrBaseSuite
}

var _ = Suite(&handlersInstallSuite{})

func (s *handlersInstallSuite) SetUpTest(c *C) {
	s.setupBaseTest(c, true)
}

func (s *handlersInstallSuite) TestDoInstallPreseed(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chroot := filepath.Join(c.MkDir(), "chroot")
	err := os.MkdirAll(chroot, 0755)
	c.Assert(err, IsNil)

	chg, err := devicestate.InstallPreseed(st, "system-label", chroot)
	c.Assert(err, IsNil)

	toolPath, err := snapdtool.InternalToolPath("snap-preseed")
	c.Assert(err, IsNil)
	mock := testutil.MockCommand(c, toolPath, "")
	defer mock.Restore()

	st.Unlock()
	s.se.Ensure()
	s.se.Wait()
	st.Lock()

	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"snap-preseed", "--hybrid", "--system-label", "system-label", chroot},
	})
}

func (s *handlersInstallSuite) TestDoInstallPreseedFromSnap(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	tmpDir := c.MkDir()
	chroot := filepath.Join(tmpDir, "chroot")
	err := os.MkdirAll(chroot, 0755)
	c.Assert(err, IsNil)

	// mock that we are running from a "snap" located in tmpDir
	snapPath := filepath.Join(tmpDir, "snap/snapd/1234")
	fakeExe := filepath.Join(snapPath, "usr/lib/snapd/snapd")

	err = os.MkdirAll(filepath.Dir(fakeExe), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(fakeExe, nil, 0755)
	c.Assert(err, IsNil)

	restore := snapdtool.MockOsReadlink(func(path string) (string, error) {
		if path != "/proc/self/exe" {
			return "", errors.New("unexpected usage")
		}
		return fakeExe, nil
	})
	defer restore()

	expectedToolPath := filepath.Join(snapPath, "usr/lib/snapd/snap-preseed")
	mock := testutil.MockCommand(c, expectedToolPath, "")
	defer mock.Restore()

	toolPath, err := snapdtool.InternalToolPath("snap-preseed")
	c.Assert(err, IsNil)
	c.Check(toolPath, Equals, expectedToolPath)

	chg, err := devicestate.InstallPreseed(st, "system-label", chroot)
	c.Assert(err, IsNil)

	st.Unlock()
	s.se.Ensure()
	s.se.Wait()
	st.Lock()

	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	c.Check(mock.Calls(), DeepEquals, [][]string{
		{"snap-preseed", "--hybrid", "--system-label", "system-label", chroot},
	})
}
