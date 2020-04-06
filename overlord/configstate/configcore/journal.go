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
	"time"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/systemd"
)

var osutilFindGid = osutil.FindGid
var sysChownPath = sys.ChownPath

func init() {
	supportedConfigurations["core.journal.persistent"] = true
}

func validateJournalSettings(tr config.ConfGetter) error {
	return validateBoolFlag(tr, "journal.persistent")
}

func handleJournalConfiguration(tr config.ConfGetter, opts *fsOnlyContext) error {
	output, err := coreCfg(tr, "journal.persistent")
	if err != nil {
		return nil
	}

	if output == "" {
		return nil
	}

	var sysd systemd.Systemd
	rootDir := dirs.GlobalRootDir
	if opts != nil {
		rootDir = opts.RootDir
	}

	logPath := filepath.Join(rootDir, "/var/log/journal")

	switch output {
	case "true":
		if err := os.MkdirAll(logPath, 0755); err != nil {
			return err
		}
		gid, err := osutilFindGid("systemd-journal")
		if err != nil {
			return fmt.Errorf("cannot find systemd-journal group: %v", err)
		}
		if err := sysChownPath(logPath, 0, sys.GroupID(gid)); err != nil {
			return err
		}
	case "false":
		if err := os.RemoveAll(logPath); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported journal.persistent option: %q", output)
	}

	if opts == nil {
		sysd = systemd.New(dirs.GlobalRootDir, systemd.SystemMode, nil)
		if err := sysd.Restart("systemd-journald", 10*time.Second); err != nil {
			return err
		}
	}

	return nil
}
