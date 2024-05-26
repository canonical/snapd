// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/seccomp/libseccomp-golang"
	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-seccomp"
	"github.com/snapcore/snapd/osutil"
)

type versionInfoSuite struct{}

var _ = Suite(&versionInfoSuite{})

func (s *versionInfoSuite) TestVersionInfo(c *C) {
	buildID := mylog.Check2(osutil.MyBuildID())


	m, i, p := seccomp.GetLibraryVersion()
	prefix := fmt.Sprintf("%s %d.%d.%d ", buildID, m, i, p)
	suffix := fmt.Sprintf(" %s", main.GoSeccompFeatures())

	defaultVi := mylog.Check2(main.VersionInfo())


	// $ echo -n 'read\nwrite\n' | sha256sum
	// 88b06efcea4b5946cebd4b0674b93744de328339de5d61b75db858119054ff93  -
	readWriteHash := "88b06efcea4b5946cebd4b0674b93744de328339de5d61b75db858119054ff93"

	c.Check(strings.HasPrefix(defaultVi, prefix), Equals, true)
	c.Check(strings.HasSuffix(defaultVi, suffix), Equals, true)
	c.Assert(len(defaultVi) > len(prefix)+len(suffix), Equals, true)
	hash := defaultVi[len(prefix) : len(defaultVi)-len(suffix)]
	c.Check(len(hash), Equals, len(readWriteHash))
	c.Check(hash, Not(Equals), readWriteHash)

	restore := main.MockSeccompSyscalls([]string{"read", "write"})
	defer restore()

	vi := mylog.Check2(main.VersionInfo())

	c.Check(vi, Equals, prefix+readWriteHash+suffix)

	// pretend it's only 'read' now
	readHash := "15fd60c6f5c6804626177d178f3dba849a41f4a1878b2e7e7e3ed38a194dc82b"
	restore = main.MockSeccompSyscalls([]string{"read"})
	defer restore()

	vi = mylog.Check2(main.VersionInfo())

	c.Check(vi, Equals, prefix+readHash+suffix)
}
