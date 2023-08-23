// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"os"
	"path/filepath"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/desktop/desktopentry"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
)

type cmdDesktopLaunch struct {
	waitMixin

	DesktopFile string `long:"desktop" required:"true"`
	Action      string `long:"action"`
	Positional  struct {
		Uris []string `positional-arg-name:"<files-or-uris>" required:"0"`
	} `positional-args:"true"`
}

func init() {
	addRoutineCommand("desktop-launch",
		i18n.G("Launch a snap application via a desktop file"),
		i18n.G("The desktop-launch command is a helper used to launch desktop entries."),
		func() flags.Commander {
			return &cmdDesktopLaunch{}
		}, nil, nil)
}

func (x *cmdDesktopLaunch) Execute([]string) error {
	if filepath.Clean(x.DesktopFile) != x.DesktopFile {
		return fmt.Errorf("desktop file has unclean path: %q", x.DesktopFile)
	}
	if !strings.HasPrefix(x.DesktopFile, dirs.SnapDesktopFilesDir+"/") {
		return fmt.Errorf("only launching snap applications from %s is supported", dirs.SnapDesktopFilesDir)
	}

	de, err := desktopentry.Read(x.DesktopFile)
	if err != nil {
		return err
	}

	uris := x.Positional.Uris

	var args []string
	if x.Action == "" {
		args, err = de.ExpandSnapExec(uris)
	} else {
		args, err = de.ExpandActionSnapExec(x.Action, uris)
	}
	if err != nil {
		return err
	}

	argv := append([]string{"snap", "run"}, args...)
	os.Setenv("BAMF_DESKTOP_FILE_HINT", x.DesktopFile)
	return syscallExec("/proc/self/exe", argv, os.Environ())
}
