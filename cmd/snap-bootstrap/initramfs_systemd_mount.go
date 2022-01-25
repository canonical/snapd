// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/systemd"
)

var (
	timeNow = time.Now

	// default 1:30, as that is how long systemd will wait for services by
	// default so seems a sensible default
	defaultMountUnitWaitTimeout = time.Minute + 30*time.Second

	unitFileDependOverride = `[Unit]
Requires=%[1]s
After=%[1]s
`

	doSystemdMount = doSystemdMountImpl
)

// systemdMountOptions reflects the set of options for mounting something using
// systemd-mount(1)
type systemdMountOptions struct {
	// Tmpfs indicates that "what" should be ignored and a new tmpfs should be
	// mounted at the location.
	Tmpfs bool
	// Ephemeral indicates that the mount should not persist from the initramfs
	// to after the pivot_root to normal userspace. The default value, false,
	// means that the mount will persist across the transition, this is done by
	// creating systemd unit overrides for various initrd targets in /run that
	// systemd understands when it isolates to the initrd-cleanup.target when
	// the pivot_root is performed.
	Ephemeral bool
	// NeedsFsck indicates that before returning to the caller, an fsck check
	// should be performed on the thing being mounted.
	NeedsFsck bool
	// NoWait will not wait until the systemd unit is active and running, which
	// is the default behavior.
	NoWait bool
	// NoSuid indicates that the partition should be mounted with nosuid set on
	// it to prevent suid execution.
	NoSuid bool
	// Bind indicates a bind mount
	Bind bool
	// Read-only mount
	ReadOnly bool
}

// doSystemdMount will mount "what" at "where" using systemd-mount(1) with
// various options. Note that in some error cases, the mount unit may have
// already been created and it will not be deleted here, if that is the case
// callers should check manually if the unit needs to be removed on error
// conditions.
func doSystemdMountImpl(what, where string, opts *systemdMountOptions) error {
	if opts == nil {
		opts = &systemdMountOptions{}
	}

	// doesn't make sense to fsck a tmpfs
	if opts.NeedsFsck && opts.Tmpfs {
		return fmt.Errorf("cannot mount %q at %q: impossible to fsck a tmpfs", what, where)
	}

	whereEscaped := systemd.EscapeUnitNamePath(where)
	unitName := whereEscaped + ".mount"

	args := []string{what, where, "--no-pager", "--no-ask-password"}
	if opts.Tmpfs {
		args = append(args, "--type=tmpfs")
	}

	if opts.NeedsFsck {
		// note that with the --fsck=yes argument, systemd will block starting
		// the mount unit on a new systemd-fsck@<what> unit that will run the
		// fsck, so we don't need to worry about waiting for that to finish in
		// the case where we are supposed to wait (which is the default for this
		// function)
		args = append(args, "--fsck=yes")
	} else {
		// the default is to use fsck=yes, so if it doesn't need fsck we need to
		// explicitly turn it off
		args = append(args, "--fsck=no")
	}

	// Under all circumstances that we use systemd-mount here from
	// snap-bootstrap, it is expected to be okay to block waiting for the unit
	// to be started and become active, because snap-bootstrap is, by design,
	// expected to run as late as possible in the initramfs, and so any
	// dependencies there might be in systemd creating and starting these mount
	// units should already be ready and so we will not block forever. If
	// however there was something going on in systemd at the same time that the
	// mount unit depended on, we could hit a deadlock blocking as systemd will
	// not enqueue this job until it's dependencies are ready, and so if those
	// things depend on this mount unit we are stuck. The solution to this
	// situation is to make snap-bootstrap run as late as possible before
	// mounting things.
	// However, we leave in the option to not block if there is ever a reason
	// we need to do so.
	if opts.NoWait {
		args = append(args, "--no-block")
	}

	var options []string
	if opts.NoSuid {
		options = append(options, "nosuid")
	}
	if opts.Bind {
		options = append(options, "bind")
	}
	if opts.ReadOnly {
		options = append(options, "ro")
	}
	if len(options) > 0 {
		args = append(args, "--options="+strings.Join(options, ","))
	}

	// note that we do not currently parse any output from systemd-mount, but if
	// we ever do, take special care surrounding the debug output that systemd
	// outputs with the "debug" kernel command line present (or equivalently the
	// SYSTEMD_LOG_LEVEL=debug env var) which will add lots of additional output
	// to stderr from systemd commands
	out, err := exec.Command("systemd-mount", args...).CombinedOutput()
	if err != nil {
		return osutil.OutputErr(out, err)
	}

	// if it should survive pivot_root() then we need to add overrides for this
	// unit to /run/systemd units
	if !opts.Ephemeral {
		// to survive the pivot_root, we need to make the mount units depend on
		// all of the various initrd special targets by adding runtime conf
		// files there
		// note we could do this statically in the initramfs main filesystem
		// layout, but that means that changes to snap-bootstrap would block on
		// waiting for those files to be added before things works here, this is
		// a more flexible strategy that puts snap-bootstrap in control
		overrideContent := []byte(fmt.Sprintf(unitFileDependOverride, unitName))
		for _, initrdUnit := range []string{
			"initrd.target",
			"initrd-fs.target",
			"initrd-switch-root.target",
			"local-fs.target",
		} {
			targetDir := filepath.Join(dirs.GlobalRootDir, "/run/systemd/system", initrdUnit+".d")
			err := os.MkdirAll(targetDir, 0755)
			if err != nil {
				return err
			}

			// add an override file for the initrd unit to depend on this mount
			// unit so that when we isolate to the initrd unit, it does not get
			// unmounted
			fname := fmt.Sprintf("snap_bootstrap_%s.conf", whereEscaped)
			err = ioutil.WriteFile(filepath.Join(targetDir, fname), overrideContent, 0644)
			if err != nil {
				return err
			}
		}
	}

	if !opts.NoWait {
		// TODO: is this necessary, systemd-mount seems to only return when the
		// unit is active and the mount is there, but perhaps we should be a bit
		// paranoid here and wait anyways?
		// see systemd-mount(1)

		// wait for the mount to exist
		start := timeNow()
		var now time.Time
		for now = timeNow(); now.Sub(start) < defaultMountUnitWaitTimeout; now = timeNow() {
			mounted, err := osutilIsMounted(where)
			if mounted {
				break
			}
			if err != nil {
				return err
			}
		}

		if now.Sub(start) > defaultMountUnitWaitTimeout {
			return fmt.Errorf("timed out after %s waiting for mount %s on %s", defaultMountUnitWaitTimeout, what, where)
		}
	}

	return nil
}
