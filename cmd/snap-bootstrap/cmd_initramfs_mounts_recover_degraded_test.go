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
	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
)

func (s *initramfsMountsSuite) TestInitramfsDegradedState(c *C) {
	tt := []struct {
		r         main.RecoverDegradedState
		encrypted bool
		degraded  bool
		comment   string
	}{
		// unencrypted happy
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuData: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuSave: main.PartitionState{
					MountState: "absent-but-optional",
				},
			},
			degraded: false,
			comment:  "happy unencrypted no save",
		},
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuData: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuSave: main.PartitionState{
					MountState: "mounted",
				},
			},
			degraded: false,
			comment:  "happy unencrypted save",
		},
		// unencrypted unhappy
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "error-mounting",
				},
				UbuntuData: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuSave: main.PartitionState{
					MountState: "absent-but-optional",
				},
				ErrorLog: []string{
					"cannot find ubuntu-boot partition on disk 259:0",
				},
			},
			degraded: true,
			comment:  "unencrypted, error mounting boot",
		},
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuData: main.PartitionState{
					MountState: "error-mounting",
				},
				UbuntuSave: main.PartitionState{
					MountState: "absent-but-optional",
				},
				ErrorLog: []string{
					"cannot find ubuntu-data partition on disk 259:0",
				},
			},
			degraded: true,
			comment:  "unencrypted, error mounting data",
		},
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuData: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuSave: main.PartitionState{
					MountState: "error-mounting",
				},
				ErrorLog: []string{
					"cannot find ubuntu-save partition on disk 259:0",
				},
			},
			degraded: true,
			comment:  "unencrypted, error mounting save",
		},

		// encrypted happy
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuData: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "run",
				},
				UbuntuSave: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "run",
				},
			},
			encrypted: true,
			degraded:  false,
			comment:   "happy encrypted",
		},
		// encrypted unhappy
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "error-mounting",
				},
				UbuntuData: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "fallback",
				},
				UbuntuSave: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "run",
				},
				ErrorLog: []string{
					"cannot find ubuntu-boot partition on disk 259:0",
				},
			},
			encrypted: true,
			degraded:  true,
			comment:   "encrypted, no boot, fallback data",
		},
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuData: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "fallback",
				},
				UbuntuSave: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "run",
				},
				ErrorLog: []string{
					"cannot unlock encrypted ubuntu-data with sealed run key: failed to unlock ubuntu-data",
				},
			},
			encrypted: true,
			degraded:  true,
			comment:   "encrypted, fallback data",
		},
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuData: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "run",
				},
				UbuntuSave: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "fallback",
				},
				ErrorLog: []string{
					"cannot unlock encrypted ubuntu-save with sealed run key: failed to unlock ubuntu-save",
				},
			},
			encrypted: true,
			degraded:  true,
			comment:   "encrypted, fallback save",
		},
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuData: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "run",
				},
				UbuntuSave: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "recovery",
				},
				ErrorLog: []string{
					"cannot unlock encrypted ubuntu-save with sealed run key: failed to unlock ubuntu-save",
				},
			},
			encrypted: true,
			degraded:  true,
			comment:   "encrypted, recovery save",
		},
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuData: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "fallback",
				},
				UbuntuSave: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "fallback",
				},
				ErrorLog: []string{
					"cannot unlock encrypted ubuntu-data with sealed run key: failed to unlock ubuntu-data",
				},
			},
			encrypted: true,
			degraded:  true,
			comment:   "encrypted, fallback data, fallback save",
		},
		{
			r: main.RecoverDegradedState{
				UbuntuBoot: main.PartitionState{
					MountState: "mounted",
				},
				UbuntuData: main.PartitionState{
					MountState:  "mounted",
					UnlockState: "unlocked",
					UnlockKey:   "fallback",
				},
				UbuntuSave: main.PartitionState{
					MountState:  "not-mounted",
					UnlockState: "not-unlocked",
				},
				ErrorLog: []string{
					"cannot unlock encrypted ubuntu-save with sealed run key: failed to unlock ubuntu-save",
					"cannot unlock encrypted ubuntu-save with sealed fallback key: failed to unlock ubuntu-save",
				},
			},
			encrypted: true,
			degraded:  true,
			comment:   "encrypted, fallback data, no save",
		},
	}

	for _, t := range tt {
		var comment CommentInterface
		if t.comment != "" {
			comment = Commentf(t.comment)
		}

		c.Assert(t.r.Degraded(t.encrypted), Equals, t.degraded, comment)
	}
}
