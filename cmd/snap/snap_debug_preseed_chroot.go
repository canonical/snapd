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

package main

import (
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/image/preseed"
)

type cmdPreseedChroot struct {
	Reset      bool `long:"reset"`
	Positional struct {
		Chroot      string `positional-arg-name:"<chroot>"`
		SystemLabel string `positional-arg-name:"<system-label>"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addDebugCommand("preseed-chroot",
		"Preseed a chroot",
		"Preseed a chroot",
		func() flags.Commander {
			return &cmdPreseedChroot{}
		}, nil, nil)
}

func (x *cmdPreseedChroot) Execute(args []string) error {
	if !x.Reset {
		if err := preseed.Hybrid(x.Positional.Chroot, x.Positional.SystemLabel); err != nil {
			return fmt.Errorf("cannot preseed hybrid system: %w", err)
		}
	} else {
		if err := preseed.HybridReset(x.Positional.Chroot, x.Positional.SystemLabel); err != nil {
			return fmt.Errorf("cannot reset hybrid system: %w", err)
		}

	}
	return nil
}
