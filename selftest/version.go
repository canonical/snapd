// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package selftest

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/strutil"
)

// checkKernelVersion looks for some unsupported configurations that users may
// encounter and provides advice on how to resolve them.
func checkKernelVersion() error {
	if release.OnClassic && release.ReleaseInfo.ID == "ubuntu" && release.ReleaseInfo.VersionID == "14.04" {
		kver := osutil.KernelVersion()
		// a kernel version looks like this: "4.4.0-112-generic" and
		// we are only interested in the bits before the "-"
		kver = strings.SplitN(kver, "-", 2)[0]
		cmp, err := strutil.VersionCompare(kver, "3.13.0")
		if err != nil {
			logger.Noticef("cannot check kernel: %v", err)
			return nil
		}
		if cmp <= 0 {
			return fmt.Errorf("you need to reboot into a 4.4 kernel to start using snapd")
		}
	}
	return nil
}
