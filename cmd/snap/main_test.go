// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"os"
	"testing"

	. "github.com/ubuntu-core/snappy/cmd/snap"
	"github.com/ubuntu-core/snappy/testutil"
	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type SnapSuite struct {
	testutil.BaseTest
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

var _ = Suite(&SnapSuite{})

func (s *SnapSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.stdout = bytes.NewBuffer(nil)
	s.stderr = bytes.NewBuffer(nil)
	Stdout = s.stdout
	Stderr = s.stderr
	s.BaseTest.AddCleanup(func() { Stdout = os.Stdout })
	s.BaseTest.AddCleanup(func() { Stderr = os.Stderr })
}
