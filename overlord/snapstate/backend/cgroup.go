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

package backend

import (
	"errors"

	"github.com/snapcore/snapd/cmd/snaplock"
	"github.com/snapcore/snapd/sandbox/cgroup"
	"github.com/snapcore/snapd/snap/naming"
)

var cgroupRemoveSnapFreezerCgroupV1 = cgroup.RemoveSnapFreezerCgroupV1

// RemoveSnapFreezerCgroup removes the snap's cgroup-v1 freezer group under the
// per-instance snap lock to avoid racing with snap-confine creating and joining
// the group. The state lock is released while acquiring and holding the snap lock.
// On cgroup-v2 systems it does nothing and does not acquire the snap lock.
func (b Backend) RemoveSnapFreezerCgroup(instanceName naming.InstanceName, stateUnlocker func() (relock func())) error {
	version, err := cgroup.Version()
	if err != nil {
		return err
	}
	if version != cgroup.V1 {
		return nil
	}
	if stateUnlocker == nil {
		return errors.New("internal error: stateUnlocker cannot be nil")
	}
	relock := stateUnlocker()
	defer relock()
	return snaplock.WithLock(instanceName.String(), func() error {
		return cgroupRemoveSnapFreezerCgroupV1(instanceName)
	})
}
