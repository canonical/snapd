// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

// Package wrappers is used to generate wrappers and service units and also desktop files for snap applications.
package wrappers

import (
	"bytes"
	"os"
	"strings"
	"text/template"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/snapenv"
)

// Doesn't need to handle complications like internal quotes, just needs to
// wrap right side of an env variable declaration with quotes for the shell.
func quoteEnvVar(envVar string) string {
	return "export " + strings.Replace(envVar, "=", "=\"", 1) + "\""
}

func generateSnapBinaryWrapper(app *snap.AppInfo) (string, error) {
	wrapperTemplate := `#!/bin/sh
set -e

# snap info
{{.EnvVars}}

if [ ! -d "$SNAP_USER_DATA" ]; then
   mkdir -p "$SNAP_USER_DATA"
fi
export HOME="$SNAP_USER_DATA"

# Snap name is: {{.App.Snap.Name}}
# App name is: {{.App.Name}}

{{.App.LauncherCommand}} "$@"
`

	if err := snap.ValidateApp(app); err != nil {
		return "", err
	}

	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(wrapperTemplate))
	wrapperData := struct {
		App     *snap.AppInfo
		EnvVars string
		// XXX: needed by snapenv
		SnapName string
		SnapArch string
		SnapPath string
		Version  string
		Revision int
		Home     string
	}{
		App: app,
		// XXX: needed by snapenv
		SnapName: app.Snap.Name(),
		SnapArch: arch.UbuntuArchitecture(),
		SnapPath: app.Snap.MountDir(),
		Version:  app.Snap.Version,
		Revision: app.Snap.Revision,
		Home:     "$HOME",
	}

	envVars := []string{}
	for _, envVar := range append(
		snapenv.GetBasicSnapEnvVars(wrapperData),
		snapenv.GetUserSnapEnvVars(wrapperData)...) {
		envVars = append(envVars, quoteEnvVar(envVar))
	}
	wrapperData.EnvVars = strings.Join(envVars, "\n")

	if err := t.Execute(&templateOut, wrapperData); err != nil {
		// this can never happen, except we forget a variable
		logger.Panicf("Unable to execute template: %v", err)
	}

	return templateOut.String(), nil
}

// AddSnapBinaries writes the wrapper binaries for the applications from the snap which aren't services.
func AddSnapBinaries(s *snap.Info) error {
	if err := os.MkdirAll(dirs.SnapBinariesDir, 0755); err != nil {
		return err
	}

	for _, app := range s.Apps {
		if app.Daemon != "" {
			continue
		}

		content, err := generateSnapBinaryWrapper(app)
		if err != nil {
			return err
		}

		if err := osutil.AtomicWriteFile(app.WrapperPath(), []byte(content), 0755, 0); err != nil {
			return err
		}
	}

	return nil
}

// RemoveSnapBinaries removes the wrapper binaries for the applications from the snap which aren't services from.
func RemoveSnapBinaries(s *snap.Info) error {
	for _, app := range s.Apps {
		os.Remove(app.WrapperPath())
	}

	return nil
}
