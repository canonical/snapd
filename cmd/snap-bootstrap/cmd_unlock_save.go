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
	"io/ioutil"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/snap"
)

func init() {
	const (
		short = "Unlock save disk"
		long  = "Unlock save disk"
	)

	addCommandBuilder(func(parser *flags.Parser) {
		if _, err := parser.AddCommand("unlock-save", short, long, &cmdUnlockSave{}); err != nil {
			panic(err)
		}
	})

	snap.SanitizePlugsSlots = func(*snap.Info) {}
}

type cmdUnlockSave struct{}

func (c *cmdUnlockSave) Execute([]string) error {
	return unlockSave()
}

func unlockSave() error {
	// FIXME: this is only valid for run mode
	model, err := getUnverifiedBootModel()
	if err != nil {
		return err
	}
	rootDir := boot.InitramfsWritableDir(model, true)
	saveKey := device.SaveKeyUnder(dirs.SnapFDEDirUnder(rootDir))
	if !osutil.FileExists(saveKey) {
		return fmt.Errorf("cannot find ubuntu-save encryption key at %v", saveKey)
	}

	key, err := ioutil.ReadFile(saveKey)
	if err != nil {
		return err
	}

	// TODO: Change secboot.UnlockVolumeUsingSealedKeyIfEncrypted to take /dev/ubuntu/save-luks instead
	disk, err := disks.DiskFromDeviceName(filepath.Join(dirs.GlobalRootDir, "/dev/ubuntu/disk"))
	if err != nil {
		return err
	}

	result, err := secbootUnlockEncryptedVolumeUsingKey(disk, "ubuntu-save", key)
	if err != nil {
		return fmt.Errorf("cannot unlock ubuntu-save volume: %v", err)
	}

	if !result.IsEncrypted {
		return fmt.Errorf("Found an unencrypted partition")
	}

	return nil
}
