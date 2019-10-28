// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package bootstrap

import (
	"fmt"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/gadget"
)

type Options struct {
	// will contain encryption later
}

func Run(gadgetRoot, device string, options *Options) error {
	if gadgetRoot == "" {
		return fmt.Errorf("cannot use empty gadget root directory")
	}
	if device == "" {
		return fmt.Errorf("cannot use empty device node")
	}

	// XXX: ensure we test that the current partition table is
	//      compatible with the gadget
	lv, err := gadget.PositionedVolumeFromGadget(gadgetRoot)
	if err != nil {
		return fmt.Errorf("cannot layout the volume: %v", err)
	}

	sfdisk := partition.NewSFDisk(device)
	created, err := sfdisk.Create(lv)
	if err != nil {
		return fmt.Errorf("cannot create the partitions: %v", err)
	}
	if err := partition.MakeFilesystems(created); err != nil {
		return err
	}

	if err := partition.DeployContent(created, gadgetRoot); err != nil {
		return err
	}

	return nil
}
