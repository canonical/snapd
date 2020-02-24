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

package main_test

import (
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-recovery-chooser"
	"github.com/snapcore/snapd/testutil"
)

type actionSuite struct {
	testutil.BaseTest
}

var _ = Suite(&actionSuite{})

func (s *actionSuite) TestActionNormalStart(c *C) {
	mf := filepath.Join(c.MkDir(), "marker")
	err := ioutil.WriteFile(mf, nil, 0644)
	c.Assert(err, IsNil)

	r := main.MockDefaultMarkerFile(mf)
	defer r()

	err = main.ExecuteMenuAction("normal-start")
	c.Assert(err, IsNil)

	c.Assert(mf, testutil.FileAbsent)
}
