// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package failover

import (
	"testing"

	check "gopkg.in/check.v1"

	. "../common"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { check.TestingT(t) }

var _ = check.Suite(&failoverSuite{})

type failoverSuite struct {
	SnappySuite
}

// The types that implement this interface can be used in the test logic
type failer interface {
	// Sets the failure conditions
	set(c *check.C)
	// Unsets the failure conditions
	unset(c *check.C)
}

// This is the logic common to all the failover tests. Each of them has define a
// type implementing the failer interface and call this function with an instance
// of it
func commonFailoverTest(c *check.C, f failer) {
	currentVersion := GetCurrentVersion(c)

	if AfterReboot(c) {
		RemoveRebootMark(c)
		f.unset(c)
		c.Assert(GetSavedVersion(c), check.Equals, currentVersion)
	} else {
		SetSavedVersion(c, currentVersion-1)
		CallFakeUpdate(c)
		f.set(c)
		Reboot(c)
	}
}
