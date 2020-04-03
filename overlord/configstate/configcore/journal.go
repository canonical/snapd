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
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/systemd"
)

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

	var storage string
	if output == "" {
		return nil
	}
	switch output {
	case "true":
		storage = "persistent"
	case "false":
		storage = "auto"
	default:
		return fmt.Errorf("unsupported journal.persistent option: %q", output)
	}

	var sysd systemd.Systemd
	rootDir := dirs.GlobalRootDir
	if opts != nil {
		rootDir = opts.RootDir
	}

	confDir := filepath.Join(rootDir, "/etc/systemd/journald.conf.d/")
	if err := os.MkdirAll(confDir, 0775); err != nil {
		return err
	}
	confFile := filepath.Join(confDir, "00-snap-core.conf")
	content := fmt.Sprintf("[Journal]\nStorage=%s\n", storage)
	if err := osutil.AtomicWriteFile(confFile, []byte(content), 0644, 0); err != nil {
		return err
	}

	if opts == nil {
		sysd = systemd.New(dirs.GlobalRootDir, systemd.SystemMode, nil)
		if err := sysd.Restart("systemd-journald", 10*time.Second); err != nil {
			return err
		}
	}

	return nil
}
