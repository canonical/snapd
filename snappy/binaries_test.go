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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/snap"
)

type binariesTestSuite struct{}

var _ = Suite(&binariesTestSuite{})

func (s *SnapTestSuite) TestGenerateBinaryName(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: foo
apps:
   foo:
   bar:
`))
	c.Assert(err, IsNil)

	c.Check(generateBinaryName(info.Apps["bar"]), Equals, filepath.Join(dirs.SnapBinariesDir, "foo.bar"))
	c.Check(generateBinaryName(info.Apps["foo"]), Equals, filepath.Join(dirs.SnapBinariesDir, "foo"))
}

const expectedWrapper = `#!/bin/sh
set -e

# snap info (deprecated)
export SNAP_APP_PATH="/snaps/pastebinit/1.4.0.0.1/"
export SNAP_APP_DATA_PATH="/var/lib/snaps/pastebinit/1.4.0.0.1/"
export SNAP_APP_USER_DATA_PATH="$HOME/snaps/pastebinit/1.4.0.0.1/"

# snap info
export SNAP="/snaps/pastebinit/1.4.0.0.1/"
export SNAP_DATA="/var/lib/snaps/pastebinit/1.4.0.0.1/"
export SNAP_NAME="pastebinit"
export SNAP_VERSION="1.4.0.0.1"
export SNAP_ARCH="%[1]s"
export SNAP_USER_DATA="$HOME/snaps/pastebinit/1.4.0.0.1/"

if [ ! -d "$SNAP_USER_DATA" ]; then
   mkdir -p "$SNAP_USER_DATA"
fi
export HOME="$SNAP_USER_DATA"

# Snap name is: pastebinit
# App name is: pastebinit

ubuntu-core-launcher pastebinit.pastebinit pastebinit_pastebinit_1.4.0.0.1 /snaps/pastebinit/1.4.0.0.1/bin/pastebinit "$@"
`

func (s *SnapTestSuite) TestSnappyGenerateSnapBinaryWrapper(c *C) {
	pkgPath := "/snaps/pastebinit/1.4.0.0.1/"
	info := &snap.Info{
		Name:    "pastebinit",
		Version: "1.4.0.0.1",
	}
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
	pkgPath := "/snaps/pastebinit.mvo/1.4.0.0.1/"
	info := &snap.Info{
		Name:    "pastebinit",
		Version: "1.4.0.0.1",
	}
	binary := &snap.AppInfo{
		Snap: info,
		Name: "bin/pastebinit\nSomething nasty",
	}

	_, err := generateSnapBinaryWrapper(binary, pkgPath)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSnappyBinPathForBinaryNoExec(c *C) {
	binary := &snap.AppInfo{Name: "pastebinit", Command: "bin/pastebinit"}
	pkgPath := "/snaps/pastebinit.mvo/1.0/"
	c.Assert(binPathForBinary(pkgPath, binary), Equals, "/snaps/pastebinit.mvo/1.0/bin/pastebinit")
}

func (s *SnapTestSuite) TestSnappyBinPathForBinaryWithExec(c *C) {
	binary := &snap.AppInfo{
		Name:    "pastebinit",
		Command: "bin/random-pastebin",
	}
	pkgPath := "/snaps/pastebinit.mvo/1.1/"
	c.Assert(binPathForBinary(pkgPath, binary), Equals, "/snaps/pastebinit.mvo/1.1/bin/random-pastebin")
}
