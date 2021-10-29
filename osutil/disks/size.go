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

package disks

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// Size returns the size of the given block device, e.g. /dev/sda1 in
// bytes as reported by the kernels BLKGETSIZE ioctl.
func Size(partDevice string) (uint64, error) {
	// Use blockdev command instead of calling the ioctl directly since
	// on 32bit systems it's a pain to get a 64bit value from a ioctl.
	raw, err := exec.Command("blockdev", "--getsz", partDevice).CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("cannot get disk size: %v", osutil.OutputErr(raw, err))
	}
	output := strings.TrimSpace(string(raw))
	partBlocks, err := strconv.ParseUint(output, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse disk size output: %v", err)
	}

	return uint64(partBlocks) * 512, nil
}
