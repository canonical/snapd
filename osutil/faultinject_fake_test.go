// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !faultinject

/*
 * Copyright (C) 2021 Canonical Ltd
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

package osutil_test

import (
	"os"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type testhelperFakeFaultInjectionSuite struct {
	testutil.BaseTest
}

var _ = Suite(&testhelperFakeFaultInjectionSuite{})

func (s *testhelperFakeFaultInjectionSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	oldSnappyTesting := os.Getenv("SNAPPY_TESTING")
	s.AddCleanup(func() { os.Setenv("SNAPPY_TESTING", oldSnappyTesting) })
	s.AddCleanup(func() { os.Unsetenv("SNAPD_FAULT_INJECT") })
}

func (s *testhelperFakeFaultInjectionSuite) TestFakeFaultInject(c *C) {
	os.Setenv("SNAPPY_TESTING", "1")

	os.Setenv("SNAPD_FAULT_INJECT", "tag:reboot,othertag:panic,funtag:reboot")
	osutil.MaybeInjectFault("tag")
	osutil.MaybeInjectFault("othertag")
	osutil.MaybeInjectFault("funtag")
}
