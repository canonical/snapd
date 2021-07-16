// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package ctlcmd

import (
	"fmt"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/systemd"
)

var (
	shortUmountHelp = i18n.G("Undo a temporary or permanent mount")
	longUmountHelp  = i18n.G(`
The umount command unmounts the given mount point.`)
)

func init() {
	addCommand("umount", shortUmountHelp, longUmountHelp, func() command { return &umountCommand{} })
}

type umountCommand struct {
	baseCommand
	Positional struct {
		Where string `positional-arg-name:"<where>" required:"yes" description:"path to the mount point"`
	} `positional-args:"yes" required:"yes"`
}

func (m *umountCommand) Execute([]string) error {
	context := m.context()
	if context == nil {
		return fmt.Errorf("Cannot mount without a context")
	}

	if m.Positional.Where == "" {
		return fmt.Errorf("Mount point cannot be empty")
	}

	snapName := context.InstanceName()

	// Get the list of all our mount units, to find the matching one
	sysd := systemd.New(systemd.SystemMode, nil)
	mountPoints, err := sysd.ListMountUnits(snapName, "")
	if err != nil {
		return fmt.Errorf("Cannot retrieve list of mount units: %v", err)
	}

	found := false
	for _, where := range mountPoints {
		if where == m.Positional.Where {
			if err := sysd.RemoveMountUnitFile(where); err != nil {
				return fmt.Errorf("Failed to remove mount unit: %v", err)
			}
			found = true
		}
	}

	if !found {
		return fmt.Errorf("Could not find the given mount")
	}

	return nil
}
