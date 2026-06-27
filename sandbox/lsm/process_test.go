// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package lsm_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/lsm"
	"github.com/snapcore/snapd/sandbox/selinux"
)

func mockBothLSMsActive() (restore func()) {
	restoreAA := lsm.MockApparmorProbedLevel(func() apparmor.LevelType {
		return apparmor.Full
	})
	restoreSE := lsm.MockSELinuxProbedLevel(func() selinux.LevelType {
		return selinux.Enforcing
	})
	return func() {
		restoreSE()
		restoreAA()
	}
}

func (*lsmSuite) TestSecurityLabelsFromPidBoth(c *C) {
	defer mockBothLSMsActive()()

	restore := lsm.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "snap.foo.app", nil
	})
	defer restore()
	restore = lsm.MockSELinuxSecurityLabelFromPid(func(int) (string, error) {
		return "system_u:system_r:snappy_t:s0", nil
	})
	defer restore()

	labels, err := lsm.SecurityLabelsFromPid(42)
	c.Assert(err, IsNil)
	c.Check(labels, DeepEquals, map[string]string{
		lsm.SecurityLabelKeyAppArmor: "snap.foo.app",
		lsm.SecurityLabelKeySELinux:  "system_u:system_r:snappy_t:s0",
	})
}

func (*lsmSuite) TestSecurityLabelsFromPidAppArmorOnly(c *C) {
	restore := lsm.MockApparmorProbedLevel(func() apparmor.LevelType {
		return apparmor.Full
	})
	defer restore()
	restore = lsm.MockSELinuxProbedLevel(func() selinux.LevelType {
		return selinux.Unsupported
	})
	defer restore()
	restore = lsm.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "snap.foo.app", nil
	})
	defer restore()

	labels, err := lsm.SecurityLabelsFromPid(42)
	c.Assert(err, IsNil)
	c.Check(labels, DeepEquals, map[string]string{
		lsm.SecurityLabelKeyAppArmor: "snap.foo.app",
	})
}

func (*lsmSuite) TestSecurityLabelsFromPidSELinuxOnly(c *C) {
	restore := lsm.MockApparmorProbedLevel(func() apparmor.LevelType {
		return apparmor.Unsupported
	})
	defer restore()
	restore = lsm.MockSELinuxProbedLevel(func() selinux.LevelType {
		return selinux.Enforcing
	})
	defer restore()
	restore = lsm.MockSELinuxSecurityLabelFromPid(func(int) (string, error) {
		return "system_u:system_r:snappy_t:s0", nil
	})
	defer restore()

	labels, err := lsm.SecurityLabelsFromPid(42)
	c.Assert(err, IsNil)
	c.Check(labels, DeepEquals, map[string]string{
		lsm.SecurityLabelKeySELinux: "system_u:system_r:snappy_t:s0",
	})
}

func (*lsmSuite) TestSecurityLabelsFromPidOmitsEmptySELinux(c *C) {
	defer mockBothLSMsActive()()

	restore := lsm.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "unconfined", nil
	})
	defer restore()
	restore = lsm.MockSELinuxSecurityLabelFromPid(func(int) (string, error) {
		return "", nil
	})
	defer restore()

	labels, err := lsm.SecurityLabelsFromPid(42)
	c.Assert(err, IsNil)
	c.Check(labels, DeepEquals, map[string]string{
		lsm.SecurityLabelKeyAppArmor: "unconfined",
	})
}

func (*lsmSuite) TestSecurityLabelsFromPidNoneActive(c *C) {
	restore := lsm.MockApparmorProbedLevel(func() apparmor.LevelType {
		return apparmor.Unsupported
	})
	defer restore()
	restore = lsm.MockSELinuxProbedLevel(func() selinux.LevelType {
		return selinux.Unsupported
	})
	defer restore()

	labels, err := lsm.SecurityLabelsFromPid(42)
	c.Assert(err, IsNil)
	c.Check(labels, DeepEquals, map[string]string{})
}

func (*lsmSuite) TestSecurityLabelsFromPidAppArmorError(c *C) {
	defer mockBothLSMsActive()()

	restore := lsm.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "", fmt.Errorf("boom")
	})
	defer restore()

	_, err := lsm.SecurityLabelsFromPid(42)
	c.Assert(err, ErrorMatches, "boom")
}

func (*lsmSuite) TestSecurityLabelFromPidAppArmor(c *C) {
	defer mockBothLSMsActive()()

	restore := lsm.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "snap.foo.app", nil
	})
	defer restore()
	restore = lsm.MockSELinuxSecurityLabelFromPid(func(int) (string, error) {
		return "system_u:system_r:snappy_t:s0", nil
	})
	defer restore()

	label, err := lsm.SecurityLabelFromPid(42)
	c.Assert(err, IsNil)
	c.Check(label, Equals, "snap.foo.app")
}

func (*lsmSuite) TestSecurityLabelFromPidSELinux(c *C) {
	defer mockBothLSMsActive()()

	restore := lsm.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "unconfined", nil
	})
	defer restore()
	restore = lsm.MockSELinuxSecurityLabelFromPid(func(int) (string, error) {
		return "system_u:system_r:snappy_t:s0", nil
	})
	defer restore()

	label, err := lsm.SecurityLabelFromPid(42)
	c.Assert(err, IsNil)
	c.Check(label, Equals, "system_u:system_r:snappy_t:s0")
}

func (*lsmSuite) TestSecurityLabelFromPidUnconfined(c *C) {
	defer mockBothLSMsActive()()

	restore := lsm.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "unconfined", nil
	})
	defer restore()
	restore = lsm.MockSELinuxSecurityLabelFromPid(func(int) (string, error) {
		return "", nil
	})
	defer restore()

	label, err := lsm.SecurityLabelFromPid(42)
	c.Assert(err, IsNil)
	c.Check(label, Equals, "unconfined")
}

func (*lsmSuite) TestSecurityLabelFromPidAppArmorError(c *C) {
	defer mockBothLSMsActive()()

	restore := lsm.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "", fmt.Errorf("boom")
	})
	defer restore()

	_, err := lsm.SecurityLabelFromPid(42)
	c.Assert(err, ErrorMatches, "boom")
}
