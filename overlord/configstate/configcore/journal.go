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

package configcore

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
)

var (
	osutilFindGid = osutil.FindGid
	sysChownPath  = sys.ChownPath
)

func init() {
	supportedConfigurations["core.journal.persistent"] = true
}

func validateJournalSettings(tr ConfGetter) error {
	return validateBoolFlag(tr, "journal.persistent")
}

func handleJournalConfiguration(_ sysconfig.Device, tr ConfGetter, opts *fsOnlyContext) error {
	output := mylog.Check2(coreCfg(tr, "journal.persistent"))

	if output == "" {
		return nil
	}

	rootDir := dirs.GlobalRootDir
	if opts != nil {
		rootDir = opts.RootDir
	}

	tempLogPath := filepath.Join(rootDir, "/var/log/journal-snapd")
	logPath := filepath.Join(rootDir, "/var/log/journal")
	marker := ".snapd-created"

	logDirExists, _, _ := osutil.DirExists(logPath)

	switch output {
	case "true":
		if logDirExists {
			// don't check marker; if the directory exists then logging is
			// already enabled (although possibly not controlled by us), but
			// don't error out. In such case we will error out if setting
			// to false is attempted.
			return nil
		}
		mylog.Check(os.MkdirAll(tempLogPath, 0755))
		mylog.Check(osutil.AtomicWriteFile(filepath.Join(tempLogPath, marker), nil, 0700, 0))
		mylog.Check(os.Rename(tempLogPath, logPath))

	case "false":
		if !logDirExists {
			return nil
		}
		// only remove journal log dir if our marker file is there
		if !osutil.FileExists(filepath.Join(logPath, marker)) {
			return fmt.Errorf("the %s directory was not created by snapd, journal logs will not be removed nor disabled", logPath)
		}
		mylog.Check(
			// This removes all logs from /var/log/journal when journal.persistent
			// is set to false.
			// It's assumed that core device is fully controlled by snapd.
			os.RemoveAll(logPath))

	default:
		return fmt.Errorf("unsupported journal.persistent option: %q", output)
	}

	if opts == nil {
		mylog.
			// old systemd-journal (e.g. on core16) closes the pipes on SIGUSR1,
			// causing SIGPIPE and restart of snapd and other services.
			// upstream bug: https://bugs.freedesktop.org/show_bug.cgi?id=84923,
			// therefore only tell journald to reload if it's new enough.
			Check(systemd.EnsureAtLeast(236))
		if err == nil {
			sysd := systemd.NewUnderRoot(dirs.GlobalRootDir, systemd.SystemMode, nil)
			mylog.Check(sysd.Kill("systemd-journald", "USR1", ""))

		} else if !systemd.IsSystemdTooOld(err) {
			// systemd not available
			return err
		}
	}

	return nil
}
