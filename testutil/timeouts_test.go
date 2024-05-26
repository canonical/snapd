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

package testutil_test

import (
	"os"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/testutil"
)

var _ = Suite(&TimeoutTestSuite{})

type TimeoutTestSuite struct{}

func mockEnvVar(envVar, value string) (restore func()) {
	oldVal, ok := os.LookupEnv(envVar)
	if value == "" {
		os.Unsetenv(envVar)
	} else {
		os.Setenv(envVar, value)
	}

	return func() {
		if ok {
			os.Setenv(envVar, oldVal)
		} else {
			os.Unsetenv(envVar)
		}
	}
}

func (ts *TimeoutTestSuite) TestHostScaledTimeout(c *C) {
	restore := mockEnvVar("GO_TEST_RACE", "")
	defer restore()

	restore = testutil.MockRuntimeARCH("default")
	defer restore()

	origDuration := 2 * time.Second

	type testcase struct {
		name     string
		setup    func() (restore func())
		expected time.Duration
	}

	testcases := []testcase{
		{
			name:     "default ",
			setup:    func() func() { return testutil.MockRuntimeARCH("some-fast-arch") },
			expected: origDuration,
		},
		{
			name:     "riscv64 arch",
			setup:    func() func() { return testutil.MockRuntimeARCH("riscv64") },
			expected: 6 * origDuration,
		},
		{
			name:     "go test -race",
			setup:    func() func() { return mockEnvVar("GO_TEST_RACE", "1") },
			expected: 5 * origDuration,
		},
		{
			name: "go test -race and riscv64 arch",
			setup: func() func() {
				archRestore := testutil.MockRuntimeARCH("riscv64")
				envVarRestore := mockEnvVar("GO_TEST_RACE", "1")
				return func() {
					archRestore()
					envVarRestore()
				}
			},
			// the arch scaling takes precedence
			expected: 6 * origDuration,
		},
	}

	for _, tc := range testcases {
		restore := tc.setup()
		out := testutil.HostScaledTimeout(origDuration)
		c.Check(out, Equals, tc.expected, Commentf("test %q failed", tc.name))
		restore()
	}
}
