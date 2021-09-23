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

package osutil

import (
	"fmt"
	"os/exec"
)

// CreateLinearMapper creates a linear device mapping of the given device
// with the given offset and size.
//
// The mapper device node is returned.
func CreateLinearMapper(device, name, uuid string, offset, size uint64) (string, error) {
	errPrefix := "cannot create mapper: "

	if offset%512 != 0 {
		return "", fmt.Errorf(errPrefix+"offset %v must be aligned to 512 bytes", offset)
	}
	if size%512 != 0 {
		return "", fmt.Errorf(errPrefix+"size %v must be aligned to 512 bytes", size)
	}
	if size <= offset {
		return "", fmt.Errorf(errPrefix+"size %v must be larger than the offset %v", size, offset)
	}

	offsetInBlocks := offset / uint64(512)
	sizeWithoutOffsetInBlocks := (size / uint64(512)) - offsetInBlocks
	dmTable := fmt.Sprintf("0 %v linear %s %v", sizeWithoutOffsetInBlocks, device, offsetInBlocks)
	cmd := exec.Command("dmsetup", "create", name)
	if uuid != "" {
		cmd.Args = append(cmd.Args, []string{"--uuid", uuid}...)
	}
	cmd.Args = append(cmd.Args, []string{"--table", dmTable}...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cannot create mapper %q on %v: %v", name, device, OutputErr(output, err))
	}

	return fmt.Sprintf("/dev/mapper/%s", name), nil
}
