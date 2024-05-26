// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd

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

package disks

import (
	"fmt"
	"os/exec"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

// CreateLinearMapperDevice creates a linear device mapping of the given device
// with the given offset and size.
//
// The total size of underlying device must be offset+size, this is
// not validated by this code.
//
// The mapper device node is returned.
func CreateLinearMapperDevice(device, name, uuid string, offset, size uint64) (string, error) {
	// TODO: eventually support 4k logical sectors, see also
	// https://bugs.launchpad.net/ubuntu/+source/lvm2/+bug/1195980
	const dmSetupSectorSize = 512

	errPrefix := fmt.Sprintf("cannot create mapper %q on %v: ", name, device)

	if offset%dmSetupSectorSize != 0 {
		return "", fmt.Errorf("%soffset %v must be aligned to %v bytes", errPrefix, offset, dmSetupSectorSize)
	}
	if size%dmSetupSectorSize != 0 {
		return "", fmt.Errorf("%ssize %v must be aligned to %v bytes", errPrefix, size, dmSetupSectorSize)
	}
	if size <= offset {
		return "", fmt.Errorf("%ssize %v must be larger than the offset %v", errPrefix, size, offset)
	}

	offsetInBlocks := offset / uint64(dmSetupSectorSize)
	sizeInBlocks := size / uint64(dmSetupSectorSize)
	dmTable := fmt.Sprintf("0 %v linear %s %v", sizeInBlocks, device, offsetInBlocks)
	cmd := exec.Command("dmsetup", "create", name)
	if uuid != "" {
		cmd.Args = append(cmd.Args, []string{"--uuid", uuid}...)
	}
	cmd.Args = append(cmd.Args, []string{"--table", dmTable}...)
	if output := mylog.Check2(cmd.CombinedOutput()); err != nil {
		return "", fmt.Errorf("%s%v", errPrefix, osutil.OutputErr(output, err))
	}

	return fmt.Sprintf("/dev/mapper/%s", name), nil
}
