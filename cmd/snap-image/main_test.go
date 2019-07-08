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

package main_test

import (
	"bytes"
	"testing"

	. "gopkg.in/check.v1"

	snap_image "github.com/snapcore/snapd/cmd/snap-image"
	"github.com/snapcore/snapd/testutil"
)

type CmdBaseTest struct {
	testutil.BaseTest

	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func (s *CmdBaseTest) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.stderr = &bytes.Buffer{}
	s.stdout = &bytes.Buffer{}

	oldStdout, oldStderr := snap_image.Stdout, snap_image.Stderr
	snap_image.Stdout = s.stdout
	snap_image.Stderr = s.stderr
	s.AddCleanup(func() {
		snap_image.Stdout = oldStdout
		snap_image.Stderr = oldStderr
	})
}

func (s *CmdBaseTest) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func TestMain(t *testing.T) {
	TestingT(t)
}
