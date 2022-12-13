// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package udev

type FakeUdev struct {
	subsystems *[]string
}

type FakeUdevDevice struct {
	subsystems *[]string
}

func (u *FakeUdev) DeviceNewFromEnvironment() (AbstractUdevDevice, error) {
	return &FakeUdevDevice{u.subsystems}, nil
}

func (u *FakeUdev) Close() {
}

func (d *FakeUdevDevice) GetParentWithSubsystemDevtype(subsystem string, devtype *string) AbstractUdevDevice {
	for _, s := range *(d.subsystems) {
		if s == subsystem {
			return &FakeUdevDevice{d.subsystems}
		}
	}
	return nil
}

func (d *FakeUdevDevice) Close() {
}

func MockUdev(subsystems *[]string) func() {
	oldNewUdev := NewUdev
	NewUdev = func() (AbstractUdev, error) {
		return &FakeUdev{subsystems}, nil
	}
	return func() {
		NewUdev = oldNewUdev
	}
}
