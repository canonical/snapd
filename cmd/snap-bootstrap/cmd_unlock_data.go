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

package main

import (
	"fmt"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
)

func init() {
	const (
		short = "Unlock data disk"
		long  = "Unlock data disk"
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("unlock-data", short, long, &cmdUnlockData{}); err != nil {
			panic(err)
		}
	})

	snap.SanitizePlugsSlots = func(*snap.Info) {}
}

type cmdUnlockData struct{}

func (c *cmdUnlockData) Execute([]string) error {
	return unlockData()
}

func unlockData() error {
	runModeKey := device.DataSealedKeyUnder(boot.InitramfsBootEncryptionKeyDir)

	opts := &secboot.UnlockVolumeUsingSealedKeyOptions{
		AllowRecoveryKey: true,
		WhichModel:       getUnverifiedBootModel,
	}

	// TODO: Change secboot.UnlockVolumeUsingSealedKeyIfEncrypted to take /dev/ubuntu/data-luks instead
	disk, err := disks.DiskFromDeviceName(filepath.Join(dirs.GlobalRootDir, "/dev/ubuntu/disk"))
	if err != nil {
		return err
	}

	result, err := secbootUnlockVolumeUsingSealedKeyIfEncrypted(disk, "ubuntu-data", runModeKey, opts)
	if err != nil {
		return err
	}
	if !result.IsEncrypted {
		return fmt.Errorf("Found unencrypted partition")
	}
	return nil
}
