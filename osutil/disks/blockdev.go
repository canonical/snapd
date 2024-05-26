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
	"strconv"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

func blockdevSizeCmd(cmd, devpath string) (uint64, error) {
	out, stderr := mylog.Check3(osutil.RunSplitOutput("blockdev", cmd, devpath))

	nospace := strings.TrimSpace(string(out))
	sz := mylog.Check2(strconv.ParseUint(nospace, 10, 64))

	return sz, nil
}

func blockDeviceSize(devpath string) (uint64, error) {
	return blockdevSizeCmd("--getsize64", devpath)
}

func blockDeviceSectorSize(devpath string) (uint64, error) {
	// the size is reported in raw bytes
	sz := mylog.Check2(blockdevSizeCmd("--getss", devpath))

	if sz == 0 {
		// in some other places we are using the sector size as a divisor (to
		// convert from bytes to sectors), so it's essential that 0 is treated
		// as an error
		return 0, fmt.Errorf("internal error: sector size returned as 0")
	}
	return sz, nil
}
