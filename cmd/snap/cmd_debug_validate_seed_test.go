// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestDebugValidateCannotValidate(c *C) {
	tmpf := filepath.Join(c.MkDir(), "seed.yaml")
	mylog.Check(os.WriteFile(tmpf, []byte(`
snaps:
 -
   name: core
   channel: stable
   file: core_6673.snap
`), 0644))


	_ = mylog.Check2(snap.Parser(snap.Client()).ParseArgs([]string{"debug", "validate-seed", tmpf}))
	c.Assert(err, ErrorMatches, `cannot validate seed:
 - no seed assertions`)
}
