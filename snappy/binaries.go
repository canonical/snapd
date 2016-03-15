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

package snappy

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/snapenv"
)

// generate the name
func generateBinaryName(m *snapYaml, app *AppYaml) string {
	var binName string
	if m.Type == snap.TypeFramework {
		binName = filepath.Base(app.Name)
	} else {
		binName = fmt.Sprintf("%s.%s", m.Name, filepath.Base(app.Name))
	}

	return filepath.Join(dirs.SnapBinariesDir, binName)
}

func binPathForBinary(pkgPath string, app *AppYaml) string {
	return filepath.Join(pkgPath, app.Command)
}

// Doesn't need to handle complications like internal quotes, just needs to
// wrap right side of an env variable declaration with quotes for the shell.
func quoteEnvVar(envVar string) string {
	return "export " + strings.Replace(envVar, "=", "=\"", 1) + "\""
}

func generateSnapBinaryWrapper(app *AppYaml, pkgPath, aaProfile string, m *snapYaml) (string, error) {
	wrapperTemplate := `#!/bin/sh
set -e

# snap info (deprecated)
{{.OldAppVars}}

# snap info
{{.NewAppVars}}

if [ ! -d "$SNAP_USER_DATA" ]; then
   mkdir -p "$SNAP_USER_DATA"
fi
export HOME="$SNAP_USER_DATA"

# Snap name is: {{.SnapName}}
# App name is: {{.AppName}}
# Developer name is: {{.Developer}}

# export old pwd
export SNAP_OLD_PWD="$(pwd)"
cd $SNAP_DATA
ubuntu-core-launcher {{.UdevAppName}} {{.AaProfile}} {{.Target}} "$@"
`

	// it's fine for this to error out; we might be in a framework or sth
	developer := developerFromBasedir(pkgPath)

	if err := verifyAppYaml(app); err != nil {
		return "", err
	}

	actualBinPath := binPathForBinary(pkgPath, app)

	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(wrapperTemplate))
	wrapperData := struct {
		SnapName    string
		AppName     string
		SnapArch    string
		SnapPath    string
		Version     string
		UdevAppName string
		Developer   string
		Home        string
		Target      string
		AaProfile   string
		OldAppVars  string
		NewAppVars  string
	}{
		SnapName:    m.Name,
		AppName:     app.Name,
		SnapArch:    arch.UbuntuArchitecture(),
		SnapPath:    pkgPath,
		Version:     m.Version,
		UdevAppName: fmt.Sprintf("%s.%s", m.Name, app.Name),
		Developer:   developer,
		Home:        "$HOME",
		Target:      actualBinPath,
		AaProfile:   aaProfile,
	}

	oldVars := []string{}
	for _, envVar := range append(
		snapenv.GetDeprecatedBasicSnapEnvVars(wrapperData),
		snapenv.GetDeprecatedUserSnapEnvVars(wrapperData)...) {
		oldVars = append(oldVars, quoteEnvVar(envVar))
	}
	wrapperData.OldAppVars = strings.Join(oldVars, "\n")

	newVars := []string{}
	for _, envVar := range append(
		snapenv.GetBasicSnapEnvVars(wrapperData),
		snapenv.GetUserSnapEnvVars(wrapperData)...) {
		newVars = append(newVars, quoteEnvVar(envVar))
	}
	wrapperData.NewAppVars = strings.Join(newVars, "\n")

	t.Execute(&templateOut, wrapperData)

	return templateOut.String(), nil
}
func addPackageBinaries(m *snapYaml, baseDir string) error {
	if err := os.MkdirAll(dirs.SnapBinariesDir, 0755); err != nil {
		return err
	}

	for _, app := range m.Apps {
		if app.Daemon != "" {
			continue
		}

		aaProfile, err := getSecurityProfile(m, app.Name, baseDir)
		if err != nil {
			return err
		}
		// this will remove the global base dir when generating the
		// service file, this ensures that /snaps/foo/1.0/bin/start
		// is in the service file when the SetRoot() option
		// is used
		realBaseDir := stripGlobalRootDir(baseDir)
		content, err := generateSnapBinaryWrapper(app, realBaseDir, aaProfile, m)
		if err != nil {
			return err
		}

		if err := osutil.AtomicWriteFile(generateBinaryName(m, app), []byte(content), 0755, 0); err != nil {
			return err
		}
	}

	return nil
}

func removePackageBinaries(m *snapYaml, baseDir string) error {
	for _, app := range m.Apps {
		os.Remove(generateBinaryName(m, app))
	}

	return nil
}
