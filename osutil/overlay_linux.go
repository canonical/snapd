// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

package osutil

import (
	"fmt"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

// isRootWritableOverlay detects if the current '/' is a writable overlay
// (fstype is 'overlay' and 'upperdir' is specified) and returns upperdir or
// the empty string if not used.
//
// Debian-based LiveCD systems use 'casper' to setup the mounts, and part of
// this setup involves running mount commands to mount / on /cow as overlay and
// results in AppArmor seeing '/upper' as the upperdir rather than '/cow/upper'
// as seen in mountinfo. By the time snapd is run, we don't have enough
// information to discover /cow through mount parent ID or st_dev (maj:min).
// While overlay doesn't use the mount source for anything itself, casper sets
// the mount source ('/cow' with the above) for its own purposes and we can
// leverage this by stripping the mount source from the beginning of upperdir.
//
// https://www.kernel.org/doc/Documentation/filesystems/overlayfs.txt
// man 5 proc
//
// Currently uses variables and Mock functions from nfs.go
var isRootWritableOverlay = func() (string, error) {
	mountinfo := mylog.Check2(LoadMountInfo())

	for _, entry := range mountinfo {
		if entry.FsType == "overlay" && entry.MountDir == "/" {
			if dir, ok := entry.SuperOptions["upperdir"]; ok {
				// upperdir must be an absolute path without
				// any AppArmor regular expression (AARE)
				// characters or double quotes to be considered
				if !strings.HasPrefix(dir, "/") || strings.ContainsAny(dir, `?*[]{}^"`) {
					continue
				}
				// if mount source is path, strip it from dir
				// (for casper)
				if strings.HasPrefix(entry.MountSource, "/") {
					dir = strings.TrimPrefix(dir, strings.TrimRight(entry.MountSource, "/"))
				}

				dir = strings.TrimRight(dir, "/")

				// The resulting trimmed dir must be an
				// absolute path that is not '/'
				if len(dir) < 2 || !strings.HasPrefix(dir, "/") {
					continue
				}

				switch dir {
				case "/media/root-rw/overlay":
					// On the Ubuntu server ephemeral image, '/' is setup via
					// overlayroot (on at least 18.10), which uses a combination
					// of overlayfs and chroot. This differs from the livecd setup
					// so special case the detection logic to look for the known
					// upperdir for this configuration, and return the required
					// path. See LP: #1797218 for details.
					return "/overlay", nil
				case "/run/miso/overlay_root/upper":
					// On the Manjaro ephemeral image, '/' is setup via
					// overlayroot. This is similar to the workaround above.
					return "/upper", nil
				}

				// Make sure trailing slashes are predictably missing
				return dir, nil
			}
		}
	}
	return "", nil
}
