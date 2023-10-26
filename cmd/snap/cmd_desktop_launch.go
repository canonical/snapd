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
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/dbusutil"
	"github.com/snapcore/snapd/desktop/desktopentry"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
)

type cmdDesktopLaunch struct {
	waitMixin

	DesktopFile string `long:"desktop" required:"true"`
	Action      string `long:"action"`
	Positional  struct {
		FilesOrUris []string `positional-arg-name:"<files-or-uris>" required:"0"`
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

func cmdlineArgsToUris(args []string) ([]string, error) {
	uris := make([]string, len(args))
	for i, arg := range args {
		// This follows the logic of glib's
		// g_file_new_for_commandline_arg function:
		//  - if it looks like an absolute path, it is a file path
		//  - if it has a valid URI scheme, it is a URI
		//  - otherwise, it is a relative file path
		if filepath.IsAbs(arg) {
			u := &url.URL{Scheme: "file", Path: arg}
			uris[i] = u.String()
		} else if u, err := url.Parse(arg); err == nil && u.IsAbs() {
			uris[i] = arg
		} else {
			fullPath, err := filepath.Abs(arg)
			if err != nil {
				return nil, err
			}
			u := &url.URL{Scheme: "file", Path: fullPath}
			uris[i] = u.String()
		}
	}
	return uris, nil
}

func collectLaunchEnv() map[string]string {
	env := map[string]string{}
	for _, key := range []string{
		"DESKTOP_STARTUP_ID",
		"XDG_ACTIVATION_TOKEN",
	} {
		if val := os.Getenv(key); val != "" {
			env[key] = val
		}
	}
	return env
}

func (x *cmdDesktopLaunch) Execute([]string) error {
	if filepath.Clean(x.DesktopFile) != x.DesktopFile {
		return fmt.Errorf("desktop file has unclean path: %q", x.DesktopFile)
	}
	if !strings.HasPrefix(x.DesktopFile, dirs.SnapDesktopFilesDir+"/") {
		return fmt.Errorf("only launching snap applications from %s is supported", dirs.SnapDesktopFilesDir)
	}

	uris, err := cmdlineArgsToUris(x.Positional.FilesOrUris)
	if err != nil {
		return err
	}

	// If running a desktop file from a confined snap process,
	// then run via the privileged launcher.
	if os.Getenv("SNAP") != "" {
		// Only the application file name is required for launching.
		desktopFile := filepath.Base(x.DesktopFile)
		env := collectLaunchEnv()

		// Attempt to launch the desktop file via the
		// privileged launcher, this will check that this snap
		// has the desktop-launch interface connected.
		conn, err := dbusutil.SessionBus()
		if err != nil {
			return fmt.Errorf(i18n.G("unable to access privileged desktop launcher: unable to get session bus: %v"), err)
		}
		o := conn.Object("io.snapcraft.Launcher", "/io/snapcraft/PrivilegedDesktopLauncher")
		call := o.Call("io.snapcraft.PrivilegedDesktopLauncher.OpenDesktopEntry2", 0, desktopFile, x.Action, uris, env)
		if call.Err != nil {
			return fmt.Errorf(i18n.G("failed to launch %s via the privileged desktop launcher: %v"), desktopFile, call.Err)
		}
		return nil
	}

	de, err := desktopentry.Read(x.DesktopFile)
	if err != nil {
		return err
	}

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
