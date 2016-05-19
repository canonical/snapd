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

package wrappers_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/wrappers"
)

type binariesWrapperGenSuite struct{}

var _ = Suite(&binariesWrapperGenSuite{})

const expectedWrapper = `#!/bin/sh
set -e

# snap info
export SNAP="/snap/pastebinit/44"
export SNAP_DATA="/var/snap/pastebinit/44"
export SNAP_NAME="pastebinit"
export SNAP_VERSION="1.4.0.0.1"
export SNAP_REVISION="44"
export SNAP_ARCH="%[1]s"
export SNAP_LIBRARY_PATH="/var/lib/snapd/lib/gl:"
export SNAP_USER_DATA="$HOME/snap/pastebinit/44"

if [ ! -d "$SNAP_USER_DATA" ]; then
   mkdir -p "$SNAP_USER_DATA"
fi
export HOME="$SNAP_USER_DATA"

# Snap name is: pastebinit
# App name is: pastebinit

/usr/bin/ubuntu-core-launcher snap.pastebinit.pastebinit snap.pastebinit.pastebinit /snap/pastebinit/44/bin/pastebinit "$@"
`

func (s *binariesWrapperGenSuite) TestSnappyGenerateSnapBinaryWrapper(c *C) {
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

	generatedWrapper, err := wrappers.GenerateSnapBinaryWrapper(binary)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expected)
}

func (s *binariesWrapperGenSuite) TestSnappyGenerateSnapBinaryWrapperIllegalChars(c *C) {
	info := &snap.Info{}
	info.SuggestedName = "pastebinit"
	info.Version = "1.4.0.0.1"
	binary := &snap.AppInfo{
		Snap: info,
		Name: "bin/pastebinit\nSomething nasty",
	}

	_, err := wrappers.GenerateSnapBinaryWrapper(binary)
	c.Assert(err, NotNil)
}
