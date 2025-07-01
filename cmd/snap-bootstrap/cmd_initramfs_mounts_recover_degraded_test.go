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

	"github.com/snapcore/snapd/boot"
)

func (s *initramfsMountsSuite) TestInitramfsDegradedState(c *C) {
	tt := []struct {
		r         main.DiskUnlockState
		encrypted bool
		degraded  bool
		comment   string
	}{
		// unencrypted happy
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "absent-but-optional",
					},
				},
			},
			degraded: false,
			comment:  "happy unencrypted no save",
		},
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
			},
			degraded: false,
			comment:  "happy unencrypted save",
		},
		// unencrypted unhappy
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "error-mounting",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "absent-but-optional",
					},
				},
			},
			degraded: true,
			comment:  "unencrypted, error mounting boot",
		},
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "error-mounting",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "absent-but-optional",
					},
				},
			},
			degraded: true,
			comment:  "unencrypted, error mounting data",
		},
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "error-mounting",
					},
				},
			},
			degraded: true,
			comment:  "unencrypted, error mounting save",
		},

		// encrypted happy
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "run",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "run",
					},
				},
			},
			encrypted: true,
			degraded:  false,
			comment:   "happy encrypted",
		},
		// encrypted unhappy
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "error-mounting",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "fallback",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "run",
					},
				},
			},
			encrypted: true,
			degraded:  true,
			comment:   "encrypted, no boot, fallback data",
		},
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "fallback",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "run",
					},
				},
			},
			encrypted: true,
			degraded:  true,
			comment:   "encrypted, fallback data",
		},
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "run",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "fallback",
					},
				},
			},
			encrypted: true,
			degraded:  true,
			comment:   "encrypted, fallback save",
		},
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "run",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "recovery",
					},
				},
			},
			encrypted: true,
			degraded:  true,
			comment:   "encrypted, recovery save",
		},
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "fallback",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "fallback",
					},
				},
			},
			encrypted: true,
			degraded:  true,
			comment:   "encrypted, fallback data, fallback save",
		},
		{
			r: main.DiskUnlockState{
				UbuntuBoot: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState: "mounted",
					},
				},
				UbuntuData: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "mounted",
						UnlockState: "unlocked",
						UnlockKey:   "fallback",
					},
				},
				UbuntuSave: main.PartitionState{
					PartitionState: boot.PartitionState{
						MountState:  "not-mounted",
						UnlockState: "not-unlocked",
					},
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
