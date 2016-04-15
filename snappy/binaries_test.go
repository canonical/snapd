// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snappy

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/snap"
)

type binariesTestSuite struct{}

var _ = Suite(&binariesTestSuite{})

const expectedWrapper = `#!/bin/sh
set -e

# snap info (deprecated)
export SNAP_APP_PATH="/snap/pastebinit/1.4.0.0.1/"
export SNAP_APP_DATA_PATH="/var/snap/pastebinit/1.4.0.0.1/"
export SNAP_APP_USER_DATA_PATH="$HOME/snap/pastebinit/1.4.0.0.1/"

# snap info
export SNAP="/snap/pastebinit/1.4.0.0.1/"
export SNAP_DATA="/var/snap/pastebinit/1.4.0.0.1/"
export SNAP_NAME="pastebinit"
export SNAP_VERSION="1.4.0.0.1"
export SNAP_REVISION="44"
export SNAP_ARCH="%[1]s"
export SNAP_LIBRARY_PATH="/var/lib/snapd/lib/gl:"
export SNAP_USER_DATA="$HOME/snap/pastebinit/1.4.0.0.1/"

if [ ! -d "$SNAP_USER_DATA" ]; then
   mkdir -p "$SNAP_USER_DATA"
fi
export HOME="$SNAP_USER_DATA"

# Snap name is: pastebinit
# App name is: pastebinit

ubuntu-core-launcher snap.pastebinit.pastebinit snap.pastebinit.pastebinit /snap/pastebinit/1.4.0.0.1/bin/pastebinit "$@"
`

func (s *SnapTestSuite) TestSnappyGenerateSnapBinaryWrapper(c *C) {
	pkgPath := "/snap/pastebinit/1.4.0.0.1/"
	info := &snap.Info{}
	info.SuggestedName = "pastebinit"
	info.Version = "1.4.0.0.1"
	info.Revision = 44
	binary := &snap.AppInfo{
		Snap:    info,
		Name:    "pastebinit",
		Command: "bin/pastebinit",
	}

	expected := fmt.Sprintf(expectedWrapper, arch.UbuntuArchitecture())

	generatedWrapper, err := generateSnapBinaryWrapper(binary, pkgPath)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expected)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapBinaryWrapperIllegalChars(c *C) {
	pkgPath := "/snap/pastebinit/1.4.0.0.1/"
	info := &snap.Info{}
	info.SuggestedName = "pastebinit"
	info.Version = "1.4.0.0.1"
	binary := &snap.AppInfo{
		Snap: info,
		Name: "bin/pastebinit\nSomething nasty",
	}

	_, err := generateSnapBinaryWrapper(binary, pkgPath)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSnappyBinPathForBinaryNoExec(c *C) {
	binary := &snap.AppInfo{Name: "pastebinit", Command: "bin/pastebinit"}
	pkgPath := "/snap/pastebinit.mvo/1.0/"
	c.Assert(binPathForBinary(pkgPath, binary), Equals, "/snap/pastebinit.mvo/1.0/bin/pastebinit")
}

func (s *SnapTestSuite) TestSnappyBinPathForBinaryWithExec(c *C) {
	binary := &snap.AppInfo{
		Name:    "pastebinit",
		Command: "bin/random-pastebin",
	}
	pkgPath := "/snap/pastebinit.mvo/1.1/"
	c.Assert(binPathForBinary(pkgPath, binary), Equals, "/snap/pastebinit.mvo/1.1/bin/random-pastebin")
}
