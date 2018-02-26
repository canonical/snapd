// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package interfaces_test

import (
	"fmt"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
)

type systemKeySuite struct {
	tmp              string
	apparmorFeatures string
	buildID          string
}

var _ = Suite(&systemKeySuite{})

func (s *systemKeySuite) SetUpTest(c *C) {
	s.tmp = c.MkDir()
	dirs.SetRootDir(s.tmp)
	s.apparmorFeatures = filepath.Join(s.tmp, "/sys/kernel/security/apparmor/features")

	id, err := osutil.MyBuildID()
	c.Assert(err, IsNil)
	s.buildID = id
}

func (s *systemKeySuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *systemKeySuite) TestInterfaceSystemKey(c *C) {
	restore := interfaces.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()

	systemKey := interfaces.SystemKey()

	apparmorFeatures := release.AppArmorFeatures()
	var apparmorFeaturesStr string
	if len(apparmorFeatures) == 0 {
		apparmorFeaturesStr = " []\n"
	} else {
		apparmorFeaturesStr = "\n- " + strings.Join(apparmorFeatures, "\n- ") + "\n"
	}
	nfsHome, err := osutil.IsHomeUsingNFS()
	c.Assert(err, IsNil)
	c.Check(systemKey, Equals, fmt.Sprintf(`build-id: %s
apparmor-features:%snfs-home: %v
`, s.buildID, apparmorFeaturesStr, nfsHome))
}

func (ts *systemKeySuite) TestInterfaceDigest(c *C) {
	restore := interfaces.MockSystemKey(`build-id: 7a94e9736c091b3984bd63f5aebfc883c4d859e0
apparmor-features:
- caps
- dbus
`)
	defer restore()
	restore = interfaces.MockIsHomeUsingNFS(func() (bool, error) { return false, nil })
	defer restore()

	systemKey := interfaces.SystemKey()
	c.Check(systemKey, Matches, "(?sm)^build-id: [a-z0-9]+$")
	c.Check(systemKey, Matches, "(?sm).*apparmor-features:")
}
