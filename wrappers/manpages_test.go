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

package wrappers_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
)

type manpagesTestSuite struct {
	testutil.BaseTest
	tempdir string
}

var _ = Suite(&manpagesTestSuite{})

func (s *manpagesTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
	s.tempdir = c.MkDir()
	dirs.SetRootDir(s.tempdir)
}

func (s *manpagesTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	s.BaseTest.TearDownTest(c)
}

func (s *manpagesTestSuite) TestAddSnapManpages(c *C) {
	// fake the thing
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(11)})
	manpage := filepath.Join(info.MountDir(), "meta", "man", "fr.UTF-8", "man1", "hello-snap.foo.1.gz")
	c.Assert(os.MkdirAll(filepath.Dir(manpage), 0755), IsNil)
	c.Assert(ioutil.WriteFile(manpage, []byte("canary"), 0644), IsNil)
	manpageFile := filepath.Join(dirs.SnapManpagesDir, "fr.UTF-8", "man1", "hello-snap.foo.1.gz")

	// sanity check
	c.Check(manpageFile, symlinkAbsent)

	// the actual test
	c.Assert(wrappers.AddSnapManpages(info), IsNil)
	c.Check(manpageFile, symlinkPresent)
	c.Check(manpageFile, testutil.FileEquals, "canary")
}

func (s *manpagesTestSuite) TestRemoveSnapManpages(c *C) {
	// fake the thing
	info := snaptest.MockSnap(c, packageHello, &snap.SideInfo{Revision: snap.R(11)})
	manpageFile := filepath.Join(dirs.SnapManpagesDir, "fr.UTF-8", "man1", "hello-snap.foo.1.gz")
	c.Assert(os.MkdirAll(filepath.Dir(manpageFile), 0755), IsNil)
	c.Assert(os.Symlink(filepath.Join(info.MountDir(), "meta", "man", "foo"), manpageFile), IsNil)

	// sanity check
	c.Check(manpageFile, symlinkPresent)

	// the actual test
	c.Assert(wrappers.RemoveSnapManpages(info), IsNil)
	c.Check(manpageFile, symlinkAbsent)
}

// helper things, find a good name and put it in testutil or sth
type symlinkPresentChecker struct{ *CheckerInfo }

var symlinkPresent Checker = &symlinkPresentChecker{CheckerInfo: &CheckerInfo{Name: "symlinkPresent", Params: []string{"filename"}}}

func (c *symlinkPresentChecker) Check(params []interface{}, names []string) (result bool, error string) {
	filename, ok := params[0].(string)
	if !ok {
		return false, "filename must be a string"
	}
	info, err := os.Lstat(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Sprintf("cannot lstat %q: %v", filename, err)
		}
		return false, fmt.Sprintf("%q does not exist", filename)
	}
	if (info.Mode() & os.ModeSymlink) == 0 {
		return false, fmt.Sprintf("%q exists but is not a symlink", filename)
	}
	return true, ""
}

type symlinkAbsentChecker struct{ *CheckerInfo }

var symlinkAbsent Checker = &symlinkAbsentChecker{CheckerInfo: &CheckerInfo{Name: "symlinkAbsent", Params: []string{"filename"}}}

func (c *symlinkAbsentChecker) Check(params []interface{}, names []string) (result bool, error string) {
	filename, ok := params[0].(string)
	if !ok {
		return false, "filename must be a string"
	}
	info, err := os.Lstat(filename)
	if err != nil {
		if !os.IsNotExist(err) {
			return false, fmt.Sprintf("cannot lstat %q: %v", filename, err)
		}
		return true, ""
	}
	if (info.Mode() & os.ModeSymlink) == 0 {
		return false, fmt.Sprintf("%q exists (not as a symlink)", filename)
	}
	return false, fmt.Sprintf("%q exists (as a symlink)", filename)
}
