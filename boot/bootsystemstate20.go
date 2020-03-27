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

package boot

import (
	"fmt"

	"github.com/snapcore/snapd/bootloader"
)

// bootSystemState20 implements the bootSystemState and bootStateUpdate
// interfaces. It is used for selecting the recovery system and mode for the
// subsequent boot.
type bootSystemState20 struct {
	bl           bootloader.Bootloader
	system, mode string
}

func newBootSystemState20() bootSystemState {
	return &bootSystemState20{}
}

func (b *bootSystemState20) setSystemMode(system, mode string) (bootStateUpdate, error) {
	if b.bl == nil {
		opts := &bootloader.Options{
			// setup the recovery bootloader
			Recovery: true,
		}
		bl, err := bootloader.Find(InitramfsUbuntuSeedDir, opts)
		if err != nil {
			return nil, err
		}
		b.bl = bl
	}
	b.system = system
	b.mode = mode
	return b, nil
}

func (b *bootSystemState20) commit() error {
	if b.system == "" || b.mode == "" {
		return fmt.Errorf("internal error: system or mode is unset")
	}

	m := map[string]string{
		"snapd_recovery_system": b.system,
		"snapd_recovery_mode":   b.mode,
	}
	return b.bl.SetBootVars(m)
}
