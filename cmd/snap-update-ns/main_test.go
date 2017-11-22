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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	update "github.com/snapcore/snapd/cmd/snap-update-ns"
	"github.com/snapcore/snapd/dirs"
)

func Test(t *testing.T) { TestingT(t) }

type mainSuite struct{}

var _ = Suite(&mainSuite{})

func (s *mainSuite) TestComputeAndSaveChanges(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer dirs.SetRootDir("/")

	restore := update.MockChangePerform(func(chg *update.Change) ([]*update.Change, error) {
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

	err = update.ComputeAndSaveChanges(snapName)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(currentProfilePath)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, `/var/lib/snapd/hostfs/usr/local/share/fonts /usr/local/share/fonts none bind,ro 0 0
/var/lib/snapd/hostfs/usr/share/fonts /usr/share/fonts none bind,ro 0 0
`)
}
