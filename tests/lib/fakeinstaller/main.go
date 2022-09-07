// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"os"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
)

func createPartitions(bootDevice string, volumes map[string]*gadget.Volume) error {
	if len(volumes) != 1 {
		return fmt.Errorf("got unexpected number of volumes %v", len(volumes))
	}

	diskLayout, err := gadget.OnDiskVolumeFromDevice(bootDevice)
	if err != nil {
		return fmt.Errorf("cannot read %v partitions: %v", bootDevice, err)
	}
	if len(diskLayout.Structure) > 0 {
		return fmt.Errorf("cannot yet install on a disk that has partitions")
	}

	constraints := gadget.LayoutConstraints{
		// XXX: cargo-culted
		NonMBRStartOffset: 1 * quantity.OffsetMiB,
		// at this point we only care about creating partitions
		SkipResolveContent:         true,
		SkipLayoutStructureContent: true,
	}

	// FIXME: refactor gadget/install code to not take these dirs
	gadgetRoot := ""
	kernelRoot := ""

	// TODO: support multiple volumes, see gadget/install/install.go
	var vol *gadget.Volume
	for _, avol := range volumes {
		vol = avol
		break
	}
	lvol, err := gadget.LayoutVolume(gadgetRoot, kernelRoot, vol, constraints)
	if err != nil {
		return fmt.Errorf("cannot layout volume: %v", err)
	}

	iconst := &install.Constraints{AllPartitions: true}
	created, err := install.CreateMissingPartitions(gadgetRoot, diskLayout, lvol, iconst)
	if err != nil {
		return fmt.Errorf("cannot create parititons: %v", err)
	}
	logger.Noticef("created %v partitions", created)

	return nil
}

func postSystemsInstallSetupStorageEncryption(details *client.SystemDetails) error {
	// TODO: check details.StorageEncryption and call POST
	// /systems/<seed-label> with "action":"install" and
	// "step":"setup-storage-encryption"
	return nil
}

func postSystemsInstallFinish(details *client.SystemDetails) error {
	// TODO: call POST /systems/<seed-label> with
	// "action":"install" and "step":"finish"
	return nil
}

func createClassicRootfsIfNeeded(bootDevice string, details *client.SystemDetails) error {
	// TODO: check model and if classic create rootfs somehow
	return nil
}

func createSeedOnTarget(bootDevice, seedLabel string) error {
	// TODO: copy installer seed to target
	return nil
}

// XXX: or will POST {"action":"install","step":"finalize"} do that?
func writeModeenvOnTarget(seedLabel string) error {
	// TODO: write modeenv
	return nil
}

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("need seed-label and target-device as argument\n")
	}
	os.Setenv("SNAPD_DEBUG", "1")
	logger.SimpleSetup()

	seedLabel := os.Args[1]
	bootDevice := os.Args[2]
	logger.Noticef("installing on %q", bootDevice)

	cli := client.New(nil)
	details, err := cli.SystemDetails(seedLabel)
	if err != nil {
		return err
	}
	// TODO: grow the data-partition based on disk size
	if err := createPartitions(bootDevice, details.Volumes); err != nil {
		return fmt.Errorf("cannot setup partitions: %v", err)
	}
	if err := postSystemsInstallSetupStorageEncryption(details); err != nil {
		return fmt.Errorf("cannot setup storage encryption: %v", err)
	}
	if err := createClassicRootfsIfNeeded(bootDevice, details); err != nil {
		return fmt.Errorf("cannot create classic rootfs: %v", err)
	}
	if err := createSeedOnTarget(bootDevice, seedLabel); err != nil {
		return fmt.Errorf("cannot create seed on target: %v", err)
	}
	// XXX: or will POST {"action":"install","step":"finalize"} do that?
	if err := writeModeenvOnTarget(seedLabel); err != nil {
		return fmt.Errorf("cannot write modeenv on target: %v", err)
	}
	// XXX: will the POST below trigger a reboot on the snapd side? if
	//      not we need to reboot here
	if err := postSystemsInstallFinish(details); err != nil {
		return fmt.Errorf("cannot finalize install: %v", err)
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
