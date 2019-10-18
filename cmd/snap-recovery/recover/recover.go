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
package recover

import (
	"fmt"

	"github.com/snapcore/snapd/cmd/snap-recovery/partition"
	"github.com/snapcore/snapd/gadget"
)

type Options struct {
	// will contain encryption later
}

type Recover struct {
	gadgetRoot string
	device     string
	options    *Options
}

func New(gadgetRoot, device string, options *Options) *Recover {
	return &Recover{
		gadgetRoot: gadgetRoot,
		device:     device,
		options:    options,
	}
}

func (r *Recover) Run() error {
	// XXX: ensure we test that the current partition table is
	//      compatible with the gadget
	lv, err := gadget.PositionedVolumeFromGadget(r.gadgetRoot)
	if err != nil {
		return fmt.Errorf("cannot layout the volume: %v", err)
	}

	sfdisk := partition.NewSFDisk(r.device)
	created, err := sfdisk.Create(lv)
	if err != nil {
		return fmt.Errorf("cannot create the partitions: %v", err)
	}
	if err := partition.MakeFilesystems(created); err != nil {
		return err
	}
	// XXX: deploy contento

	return nil
}
