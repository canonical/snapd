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

package main_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type mainSuite struct {
	testutil.BaseTest
	as  *update.Assumptions
	log *bytes.Buffer
}

var _ = Suite(&mainSuite{})

func (s *mainSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.as = &update.Assumptions{}
	buf, restore := logger.MockLogger()
	s.BaseTest.AddCleanup(restore)
	s.log = buf
}

func (s *mainSuite) TestComputeAndSaveChanges(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		return nil, nil
	})
	defer restore()

	snapName := "foo"
	desiredProfileContent := `/var/lib/snapd/hostfs/usr/share/fonts /usr/share/fonts none bind,ro 0 0
/var/lib/snapd/hostfs/usr/local/share/fonts /usr/local/share/fonts none bind,ro 0 0`

	desiredProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapMountPolicyDir, snapName)
	err := os.MkdirAll(filepath.Dir(desiredProfilePath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(desiredProfilePath, []byte(desiredProfileContent), 0644)
	c.Assert(err, IsNil)

	currentProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapRunNsDir, snapName)
	err = os.MkdirAll(filepath.Dir(currentProfilePath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(currentProfilePath, nil, 0644)
	c.Assert(err, IsNil)

	err = update.ComputeAndSaveChanges(snapName, s.as)
	c.Assert(err, IsNil)

	c.Check(currentProfilePath, testutil.FileEquals, `/var/lib/snapd/hostfs/usr/local/share/fonts /usr/local/share/fonts none bind,ro 0 0
/var/lib/snapd/hostfs/usr/share/fonts /usr/share/fonts none bind,ro 0 0
`)
}

func (s *mainSuite) TestAddingSyntheticChanges(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	// The snap `mysnap` wishes to export it's usr/share/mysnap directory and
	// make it appear as if it was in /usr/share/mysnap directly.
	const snapName = "mysnap"
	const currentProfileContent = ""
	const desiredProfileContent = "/snap/mysnap/42/usr/share/mysnap /usr/share/mysnap none bind,ro 0 0"

	currentProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapRunNsDir, snapName)
	desiredProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapMountPolicyDir, snapName)

	c.Assert(os.MkdirAll(filepath.Dir(currentProfilePath), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(desiredProfilePath), 0755), IsNil)
	c.Assert(ioutil.WriteFile(currentProfilePath, []byte(currentProfileContent), 0644), IsNil)
	c.Assert(ioutil.WriteFile(desiredProfilePath, []byte(desiredProfileContent), 0644), IsNil)

	// In order to make that work, /usr/share had to be converted to a writable
	// mimic. Some actions were performed under the hood and now we see a
	// subset of them as synthetic changes here.
	//
	// Note that if you compare this to the code that plans a writable mimic
	// you will see that there are additional changes that are _not_
	// represented here. The changes have only one goal: tell
	// snap-update-ns how the mimic can be undone in case it is no longer
	// needed.
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		// The change that we were asked to perform is to create a bind mount
		// from within the snap to /usr/share/mysnap.
		c.Assert(chg, DeepEquals, &update.Change{
			Action: update.Mount, Entry: osutil.MountEntry{
				Name: "/snap/mysnap/42/usr/share/mysnap",
				Dir:  "/usr/share/mysnap", Type: "none",
				Options: []string{"bind", "ro"}}})
		synthetic := []*update.Change{
			// The original directory (which was a part of the core snap and is
			// read only) was hidden with a tmpfs.
			{Action: update.Mount, Entry: osutil.MountEntry{
				Dir: "/usr/share", Name: "tmpfs", Type: "tmpfs",
				Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/usr/share/mysnap"}}},
			// For the sake of brevity we will only represent a few of the
			// entries typically there. Normally this list can get quite long.
			// Also note that the entry is a little fake. In reality it was
			// constructed using a temporary bind mount that contained the
			// original mount entries of /usr/share but this fact was lost.
			// Again, the only point of this entry is to correctly perform an
			// undo operation when /usr/share/mysnap is no longer needed.
			{Action: update.Mount, Entry: osutil.MountEntry{
				Dir: "/usr/share/adduser", Name: "/usr/share/adduser",
				Options: []string{"bind", "ro", "x-snapd.synthetic", "x-snapd.needed-by=/usr/share/mysnap"}}},
			{Action: update.Mount, Entry: osutil.MountEntry{
				Dir: "/usr/share/awk", Name: "/usr/share/awk",
				Options: []string{"bind", "ro", "x-snapd.synthetic", "x-snapd.needed-by=/usr/share/mysnap"}}},
		}
		return synthetic, nil
	})
	defer restore()

	c.Assert(update.ComputeAndSaveChanges(snapName, s.as), IsNil)

	c.Check(currentProfilePath, testutil.FileEquals,
		`tmpfs /usr/share tmpfs x-snapd.synthetic,x-snapd.needed-by=/usr/share/mysnap 0 0
/usr/share/adduser /usr/share/adduser none bind,ro,x-snapd.synthetic,x-snapd.needed-by=/usr/share/mysnap 0 0
/usr/share/awk /usr/share/awk none bind,ro,x-snapd.synthetic,x-snapd.needed-by=/usr/share/mysnap 0 0
/snap/mysnap/42/usr/share/mysnap /usr/share/mysnap none bind,ro 0 0
`)
}

func (s *mainSuite) TestRemovingSyntheticChanges(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	// The snap `mysnap` no longer wishes to export it's usr/share/mysnap
	// directory. All the synthetic changes that were associated with that mount
	// entry can be discarded.
	const snapName = "mysnap"
	const currentProfileContent = `tmpfs /usr/share tmpfs x-snapd.synthetic,x-snapd.needed-by=/usr/share/mysnap 0 0
/usr/share/adduser /usr/share/adduser none bind,ro,x-snapd.synthetic,x-snapd.needed-by=/usr/share/mysnap 0 0
/usr/share/awk /usr/share/awk none bind,ro,x-snapd.synthetic,x-snapd.needed-by=/usr/share/mysnap 0 0
/snap/mysnap/42/usr/share/mysnap /usr/share/mysnap none bind,ro 0 0
`
	const desiredProfileContent = ""

	currentProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapRunNsDir, snapName)
	desiredProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapMountPolicyDir, snapName)

	c.Assert(os.MkdirAll(filepath.Dir(currentProfilePath), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(desiredProfilePath), 0755), IsNil)
	c.Assert(ioutil.WriteFile(currentProfilePath, []byte(currentProfileContent), 0644), IsNil)
	c.Assert(ioutil.WriteFile(desiredProfilePath, []byte(desiredProfileContent), 0644), IsNil)

	n := -1
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		n++
		switch n {
		case 0:
			c.Assert(chg, DeepEquals, &update.Change{
				Action: update.Unmount,
				Entry: osutil.MountEntry{
					Name: "/snap/mysnap/42/usr/share/mysnap",
					Dir:  "/usr/share/mysnap", Type: "none",
					Options: []string{"bind", "ro"},
				},
			})
		case 1:
			c.Assert(chg, DeepEquals, &update.Change{
				Action: update.Unmount,
				Entry: osutil.MountEntry{
					Name: "/usr/share/awk", Dir: "/usr/share/awk", Type: "none",
					Options: []string{"bind", "ro", "x-snapd.synthetic", "x-snapd.needed-by=/usr/share/mysnap"},
				},
			})
		case 2:
			c.Assert(chg, DeepEquals, &update.Change{
				Action: update.Unmount,
				Entry: osutil.MountEntry{
					Name: "/usr/share/adduser", Dir: "/usr/share/adduser", Type: "none",
					Options: []string{"bind", "ro", "x-snapd.synthetic", "x-snapd.needed-by=/usr/share/mysnap"},
				},
			})
		case 3:
			c.Assert(chg, DeepEquals, &update.Change{
				Action: update.Unmount,
				Entry: osutil.MountEntry{
					Name: "tmpfs", Dir: "/usr/share", Type: "tmpfs",
					Options: []string{"x-snapd.synthetic", "x-snapd.needed-by=/usr/share/mysnap"},
				},
			})
		default:
			panic(fmt.Sprintf("unexpected call n=%d, chg: %v", n, *chg))
		}
		return nil, nil
	})
	defer restore()

	c.Assert(update.ComputeAndSaveChanges(snapName, s.as), IsNil)

	c.Check(currentProfilePath, testutil.FileEquals, "")
}

func (s *mainSuite) TestApplyingLayoutChanges(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	const snapName = "mysnap"
	const currentProfileContent = ""
	const desiredProfileContent = "/snap/mysnap/42/usr/share/mysnap /usr/share/mysnap none bind,ro,x-snapd.origin=layout 0 0"

	currentProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapRunNsDir, snapName)
	desiredProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapMountPolicyDir, snapName)

	c.Assert(os.MkdirAll(filepath.Dir(currentProfilePath), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(desiredProfilePath), 0755), IsNil)
	c.Assert(ioutil.WriteFile(currentProfilePath, []byte(currentProfileContent), 0644), IsNil)
	c.Assert(ioutil.WriteFile(desiredProfilePath, []byte(desiredProfileContent), 0644), IsNil)

	n := -1
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		n++
		switch n {
		case 0:
			c.Assert(chg, DeepEquals, &update.Change{
				Action: update.Mount,
				Entry: osutil.MountEntry{
					Name: "/snap/mysnap/42/usr/share/mysnap",
					Dir:  "/usr/share/mysnap", Type: "none",
					Options: []string{"bind", "ro", "x-snapd.origin=layout"},
				},
			})
			return nil, fmt.Errorf("testing")
		default:
			panic(fmt.Sprintf("unexpected call n=%d, chg: %v", n, *chg))
		}
	})
	defer restore()

	// The error was not ignored, we bailed out.
	c.Assert(update.ComputeAndSaveChanges(snapName, s.as), ErrorMatches, "testing")

	c.Check(currentProfilePath, testutil.FileEquals, "")
}

func (s *mainSuite) TestApplyIgnoredMissingMount(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	const snapName = "mysnap"
	const currentProfileContent = ""
	const desiredProfileContent = "/source /target none bind,x-snapd.ignore-missing 0 0"

	currentProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapRunNsDir, snapName)
	desiredProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapMountPolicyDir, snapName)

	c.Assert(os.MkdirAll(filepath.Dir(currentProfilePath), 0755), IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(desiredProfilePath), 0755), IsNil)
	c.Assert(ioutil.WriteFile(currentProfilePath, []byte(currentProfileContent), 0644), IsNil)
	c.Assert(ioutil.WriteFile(desiredProfilePath, []byte(desiredProfileContent), 0644), IsNil)

	n := -1
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		n++
		switch n {
		case 0:
			c.Assert(chg, DeepEquals, &update.Change{
				Action: update.Mount,
				Entry: osutil.MountEntry{
					Name:    "/source",
					Dir:     "/target",
					Type:    "none",
					Options: []string{"bind", "x-snapd.ignore-missing"},
				},
			})
			return nil, update.ErrIgnoredMissingMount
		default:
			panic(fmt.Sprintf("unexpected call n=%d, chg: %v", n, *chg))
		}
	})
	defer restore()

	// The error was ignored, and no mount was recorded in the profile
	c.Assert(update.ComputeAndSaveChanges(snapName, s.as), IsNil)
	c.Check(s.log.String(), Equals, "")
	c.Check(currentProfilePath, testutil.FileEquals, "")
}

func (s *mainSuite) TestApplyUserFstab(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	var changes []update.Change
	restore := update.MockChangePerform(func(chg *update.Change, as *update.Assumptions) ([]*update.Change, error) {
		changes = append(changes, *chg)
		return nil, nil
	})
	defer restore()

	snapName := "foo"
	desiredProfileContent := `$XDG_RUNTIME_DIR/doc/by-app/snap.foo $XDG_RUNTIME_DIR/doc none bind,rw 0 0`

	desiredProfilePath := fmt.Sprintf("%s/snap.%s.user-fstab", dirs.SnapMountPolicyDir, snapName)
	err := os.MkdirAll(filepath.Dir(desiredProfilePath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(desiredProfilePath, []byte(desiredProfileContent), 0644)
	c.Assert(err, IsNil)

	err = update.ApplyUserFstab("foo")
	c.Assert(err, IsNil)

	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())

	c.Assert(changes, HasLen, 1)
	c.Assert(changes[0].Action, Equals, update.Mount)
	c.Assert(changes[0].Entry.Name, Equals, xdgRuntimeDir+"/doc/by-app/snap.foo")
	c.Assert(changes[0].Entry.Dir, Matches, xdgRuntimeDir+"/doc")
}
