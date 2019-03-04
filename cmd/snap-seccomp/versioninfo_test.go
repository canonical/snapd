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

	. "gopkg.in/check.v1"

	"github.com/mvo5/libseccomp-golang"

	main "github.com/snapcore/snapd/cmd/snap-seccomp"
)

type versionInfoSuite struct{}

var _ = Suite(&versionInfoSuite{})

func (s *versionInfoSuite) TestVersionInfo(c *C) {
	m, i, p := seccomp.GetLibraryVersion()

	restore := main.MockSeccompSyscalls([]string{"read", "write"})
	defer restore()

	// $ echo -n 'readwrite' | sha256sum
	// dbed7fe3ca011c3d1fb0fec3bdced5031d4ef17dfce2fa867717f7beeff23d8e  -
	syscallsHash := "dbed7fe3ca011c3d1fb0fec3bdced5031d4ef17dfce2fa867717f7beeff23d8e"

	vi := main.VersionInfo()
	c.Assert(vi, Equals, fmt.Sprintf("%d.%d.%d %s", m, i, p, syscallsHash))
}
