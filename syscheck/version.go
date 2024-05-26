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

package syscheck

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/strutil"
)

func init() {
	checks = append(checks, checkKernelVersion)
}

// supportsMayDetachMounts checks whether a RHEL 7.4+ specific kernel knob is present
// and set to proper value
func supportsMayDetachMounts(kver string) error {
	p := filepath.Join(dirs.GlobalRootDir, "/proc/sys/fs/may_detach_mounts")
	value := mylog.Check2(os.ReadFile(p))

	if !bytes.Equal(value, []byte("1\n")) {
		return fmt.Errorf("fs.may_detach_mounts kernel parameter is supported but disabled")
	}
	return nil
}

// checkKernelVersion looks for some unsupported configurations that users may
// encounter and provides advice on how to resolve them.
func checkKernelVersion() error {
	if !release.OnClassic {
		return nil
	}

	switch release.ReleaseInfo.ID {
	case "ubuntu":
		if release.ReleaseInfo.VersionID == "14.04" {
			kver := osutil.KernelVersion()
			// a kernel version looks like this: "4.4.0-112-generic" and
			// we are only interested in the bits before the "-"
			kver = strings.SplitN(kver, "-", 2)[0]
			cmp := mylog.Check2(strutil.VersionCompare(kver, "3.13.0"))

			if cmp <= 0 {
				return fmt.Errorf("you need to reboot into a 4.4 kernel to start using snapd")
			}
		}
	case "rhel", "centos":
		// check for kernel tweaks on RHEL/CentOS 7.5+
		// CentoS 7.5 has VERSION_ID="7", RHEL 7.6 has VERSION_ID="7.6"
		if release.ReleaseInfo.VersionID == "" || release.ReleaseInfo.VersionID[0] != '7' {
			return nil
		}
		fullKver := osutil.KernelVersion()
		// kernel version looks like this: "3.10.0-957.el7.x86_64"
		kver := strings.SplitN(fullKver, "-", 2)[0]
		cmp := mylog.Check2(strutil.VersionCompare(kver, "3.18.0"))

		if cmp < 0 {
			// pre 3.18 kernels here
			if idx := strings.Index(fullKver, ".el7."); idx == -1 {
				// non stock kernel, assume it's not supported
				return fmt.Errorf("unsupported kernel version %q, you need to switch to the stock kernel", fullKver)
			}
			// stock kernel had bugfixes backported to it
			return supportsMayDetachMounts(kver)
		}
	}
	return nil
}
