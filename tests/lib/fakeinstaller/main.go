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
	if len(details.Volumes) != 1 {
		return fmt.Errorf("got unexpected number of volumes %v", len(details.Volumes))
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
	for _, avol := range details.Volumes {
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

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
