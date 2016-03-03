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
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/systemd"
)

// wait this time between TERM and KILL
var killWait = 5 * time.Second

// servicesBinariesStringsWhitelist is the whitelist of legal chars
// in the "binaries" and "services" section of the snap.yaml
var servicesBinariesStringsWhitelist = regexp.MustCompile(`^[A-Za-z0-9/. _#:-]*$`)

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

func verifyAppYaml(app *AppYaml) error {
	contains := func(needle string, haystack []string) bool {
		for _, h := range haystack {
			if needle == h {
				return true
			}
		}
		return false
	}
	valid := []string{"", "simple", "forking", "oneshot", "dbus"}
	if !contains(app.Daemon, valid) {
		return fmt.Errorf(`"daemon" field contains invalid value %q`, app.Daemon)
	}

	return verifyStructStringsAgainstWhitelist(*app, servicesBinariesStringsWhitelist)
}

func verifyUsesYaml(uses *usesYaml) error {
	if err := verifyStructStringsAgainstWhitelist(*uses, servicesBinariesStringsWhitelist); err != nil {
		return err
	}

	if uses.Type != "migration-skill" {
		return fmt.Errorf("can not use skill %q, only migration-skill supported", uses.Type)
	}

	return nil
}

// Doesn't need to handle complications like internal quotes, just needs to
// wrap right side of an env variable declaration with quotes for the shell.
func quoteEnvVar(envVar string) string {
	return "export " + strings.Replace(envVar, "=", "=\"", 1) + "\""
}

func generateSnapBinaryWrapper(app *AppYaml, pkgPath, aaProfile string, m *snapYaml) (string, error) {
	wrapperTemplate := `#!/bin/sh
set -e

# app info (deprecated)
{{.OldAppVars}}

# app info
{{.NewAppVars}}

if [ ! -d "$SNAP_USER_DATA" ]; then
   mkdir -p "$SNAP_USER_DATA"
fi
export HOME="$SNAP_USER_DATA"

# export old pwd
export SNAP_OLD_PWD="$(pwd)"
cd {{.AppPath}}
ubuntu-core-launcher {{.UdevAppName}} {{.AaProfile}} {{.Target}} "$@"
`

	// it's fine for this to error out; we might be in a framework or sth
	origin := originFromBasedir(pkgPath)

	if err := verifyAppYaml(app); err != nil {
		return "", err
	}

	actualBinPath := binPathForBinary(pkgPath, app)
	udevPartName := m.qualifiedName(origin)

	var templateOut bytes.Buffer
	t := template.Must(template.New("wrapper").Parse(wrapperTemplate))
	wrapperData := struct {
		AppName     string
		AppArch     string
		AppPath     string
		Version     string
		UdevAppName string
		Origin      string
		Home        string
		Target      string
		AaProfile   string
		OldAppVars  string
		NewAppVars  string
	}{
		AppName:     m.Name,
		AppArch:     arch.UbuntuArchitecture(),
		AppPath:     pkgPath,
		Version:     m.Version,
		UdevAppName: udevPartName,
		Origin:      origin,
		Home:        "$HOME",
		Target:      actualBinPath,
		AaProfile:   aaProfile,
	}

	oldVars := []string{}
	for _, envVar := range append(
		helpers.GetDeprecatedBasicSnapEnvVars(wrapperData),
		helpers.GetDeprecatedUserSnapEnvVars(wrapperData)...) {
		oldVars = append(oldVars, quoteEnvVar(envVar))
	}
	wrapperData.OldAppVars = strings.Join(oldVars, "\n")

	newVars := []string{}
	for _, envVar := range append(
		helpers.GetBasicSnapEnvVars(wrapperData),
		helpers.GetUserSnapEnvVars(wrapperData)...) {
		newVars = append(newVars, quoteEnvVar(envVar))
	}
	wrapperData.NewAppVars = strings.Join(newVars, "\n")

	t.Execute(&templateOut, wrapperData)

	return templateOut.String(), nil
}

// FIXME: too much magic, just do explicit validation of the few
//        fields we have
// verifyStructStringsAgainstWhitelist takes a struct and ensures that
// the given whitelist regexp matches all string fields of the struct
func verifyStructStringsAgainstWhitelist(s interface{}, whitelist *regexp.Regexp) error {
	// check all members of the services struct against our whitelist
	t := reflect.TypeOf(s)
	v := reflect.ValueOf(s)
	for i := 0; i < t.NumField(); i++ {

		// PkgPath means its a unexported field and we can ignore it
		if t.Field(i).PkgPath != "" {
			continue
		}
		if v.Field(i).Kind() == reflect.Ptr {
			vi := v.Field(i).Elem()
			if vi.Kind() == reflect.Struct {
				return verifyStructStringsAgainstWhitelist(vi.Interface(), whitelist)
			}
		}
		if v.Field(i).Kind() == reflect.Struct {
			vi := v.Field(i).Interface()
			return verifyStructStringsAgainstWhitelist(vi, whitelist)
		}
		if v.Field(i).Kind() == reflect.String {
			key := t.Field(i).Name
			value := v.Field(i).String()
			if !whitelist.MatchString(value) {
				return &ErrStructIllegalContent{
					Field:     key,
					Content:   value,
					Whitelist: whitelist.String(),
				}
			}
		}
	}

	return nil
}

func generateSnapServicesFile(app *AppYaml, baseDir string, aaProfile string, m *snapYaml) (string, error) {
	if err := verifyAppYaml(app); err != nil {
		return "", err
	}

	udevPartName := m.qualifiedName(originFromBasedir(baseDir))

	desc := app.Description
	if desc == "" {
		desc = fmt.Sprintf("service %s for package %s", app.Name, m.Name)
	}

	socketFileName := ""
	if app.Socket {
		socketFileName = filepath.Base(generateSocketFileName(m, app))
	}

	return systemd.New(dirs.GlobalRootDir, nil).GenServiceFile(
		&systemd.ServiceDescription{
			AppName:        m.Name,
			ServiceName:    app.Name,
			Version:        m.Version,
			Description:    desc,
			AppPath:        baseDir,
			Start:          app.Command,
			Stop:           app.Stop,
			PostStop:       app.PostStop,
			StopTimeout:    time.Duration(app.StopTimeout),
			AaProfile:      aaProfile,
			IsFramework:    m.Type == snap.TypeFramework,
			IsNetworked:    app.Ports != nil && len(app.Ports.External) > 0,
			BusName:        app.BusName,
			Type:           app.Daemon,
			UdevAppName:    udevPartName,
			Socket:         app.Socket,
			SocketFileName: socketFileName,
			Restart:        app.RestartCond,
		}), nil
}
func generateSnapSocketFile(app *AppYaml, baseDir string, aaProfile string, m *snapYaml) (string, error) {
	if err := verifyAppYaml(app); err != nil {
		return "", err
	}

	// lp: #1515709, systemd will default to 0666 if no socket mode
	// is specified
	if app.SocketMode == "" {
		app.SocketMode = "0660"
	}

	serviceFileName := filepath.Base(generateServiceFileName(m, app))

	return systemd.New(dirs.GlobalRootDir, nil).GenSocketFile(
		&systemd.ServiceDescription{
			ServiceFileName: serviceFileName,
			ListenStream:    app.ListenStream,
			SocketMode:      app.SocketMode,
			SocketUser:      app.SocketUser,
			SocketGroup:     app.SocketGroup,
		}), nil
}

func generateServiceFileName(m *snapYaml, app *AppYaml) string {
	return filepath.Join(dirs.SnapServicesDir, fmt.Sprintf("%s_%s_%s.service", m.Name, app.Name, m.Version))
}

func generateSocketFileName(m *snapYaml, app *AppYaml) string {
	return filepath.Join(dirs.SnapServicesDir, fmt.Sprintf("%s_%s_%s.socket", m.Name, app.Name, m.Version))
}

func generateBusPolicyFileName(m *snapYaml, app *AppYaml) string {
	return filepath.Join(dirs.SnapBusPolicyDir, fmt.Sprintf("%s_%s_%s.conf", m.Name, app.Name, m.Version))
}

// takes a directory and removes the global root, this is needed
// when the SetRoot option is used and we need to generate
// content for the "Apps" section
var stripGlobalRootDir = stripGlobalRootDirImpl

func stripGlobalRootDirImpl(dir string) string {
	if dirs.GlobalRootDir == "/" {
		return dir
	}

	return dir[len(dirs.GlobalRootDir):]
}

func addPackageServices(m *snapYaml, baseDir string, inhibitHooks bool, inter interacter) error {
	for _, app := range m.Apps {
		if app.Daemon == "" {
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
		// Generate service file
		content, err := generateSnapServicesFile(app, realBaseDir, aaProfile, m)
		if err != nil {
			return err
		}
		serviceFilename := generateServiceFileName(m, app)
		os.MkdirAll(filepath.Dir(serviceFilename), 0755)
		if err := helpers.AtomicWriteFile(serviceFilename, []byte(content), 0644, 0); err != nil {
			return err
		}
		// Generate systemd socket file if needed
		if app.Socket {
			content, err := generateSnapSocketFile(app, realBaseDir, aaProfile, m)
			if err != nil {
				return err
			}
			socketFilename := generateSocketFileName(m, app)
			os.MkdirAll(filepath.Dir(socketFilename), 0755)
			if err := helpers.AtomicWriteFile(socketFilename, []byte(content), 0644, 0); err != nil {
				return err
			}
		}
		// If necessary, generate the DBus policy file so the framework
		// service is allowed to start
		if m.Type == snap.TypeFramework && app.BusName != "" {
			content, err := genBusPolicyFile(app.BusName)
			if err != nil {
				return err
			}
			policyFilename := generateBusPolicyFileName(m, app)
			os.MkdirAll(filepath.Dir(policyFilename), 0755)
			if err := helpers.AtomicWriteFile(policyFilename, []byte(content), 0644, 0); err != nil {
				return err
			}
		}

		// daemon-reload and start only if we are not in the
		// inhibitHooks mode
		//
		// *but* always run enable (which just sets a symlink)
		serviceName := filepath.Base(generateServiceFileName(m, app))
		sysd := systemd.New(dirs.GlobalRootDir, inter)
		if !inhibitHooks {
			if err := sysd.DaemonReload(); err != nil {
				return err
			}
		}

		// we always enable the service even in inhibit hooks
		if err := sysd.Enable(serviceName); err != nil {
			return err
		}

		if !inhibitHooks {
			if err := sysd.Start(serviceName); err != nil {
				return err
			}
		}

		if app.Socket {
			socketName := filepath.Base(generateSocketFileName(m, app))
			// we always enable the socket even in inhibit hooks
			if err := sysd.Enable(socketName); err != nil {
				return err
			}

			if !inhibitHooks {
				if err := sysd.Start(socketName); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func removePackageServices(m *snapYaml, baseDir string, inter interacter) error {
	sysd := systemd.New(dirs.GlobalRootDir, inter)
	for _, app := range m.Apps {
		if app.Daemon == "" {
			continue
		}

		serviceName := filepath.Base(generateServiceFileName(m, app))
		if err := sysd.Disable(serviceName); err != nil {
			return err
		}
		if err := sysd.Stop(serviceName, time.Duration(app.StopTimeout)); err != nil {
			if !systemd.IsTimeout(err) {
				return err
			}
			inter.Notify(fmt.Sprintf("%s refused to stop, killing.", serviceName))
			// ignore errors for kill; nothing we'd do differently at this point
			sysd.Kill(serviceName, "TERM")
			time.Sleep(killWait)
			sysd.Kill(serviceName, "KILL")
		}

		if err := os.Remove(generateServiceFileName(m, app)); err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove service file for %q: %v", serviceName, err)
		}

		if err := os.Remove(generateSocketFileName(m, app)); err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove socket file for %q: %v", serviceName, err)
		}

		// Also remove DBus system policy file
		if err := os.Remove(generateBusPolicyFileName(m, app)); err != nil && !os.IsNotExist(err) {
			logger.Noticef("Failed to remove bus policy file for service %q: %v", serviceName, err)
		}
	}

	// only reload if we actually had services
	// FIXME: filter for services
	if len(m.Apps) > 0 {
		if err := sysd.DaemonReload(); err != nil {
			return err
		}
	}

	return nil
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

		if err := helpers.AtomicWriteFile(generateBinaryName(m, app), []byte(content), 0755, 0); err != nil {
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

type agreer interface {
	Agreed(intro, license string) bool
}

type interacter interface {
	agreer
	Notify(status string)
}

// FIXME: kill once every test is converted
func installClick(snapFilePath string, flags InstallFlags, inter progress.Meter, origin string) (name string, err error) {
	overlord := &Overlord{}
	snapPart, err := overlord.Install(snapFilePath, origin, flags, inter)
	if err != nil {
		return "", err
	}

	return snapPart.Name(), nil
}

// removeSnapData removes the data for the given version of the given snap
func removeSnapData(fullName, version string) error {
	dirs, err := snapDataDirs(fullName, version)
	if err != nil {
		return err
	}

	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
			return err
		}
		os.Remove(filepath.Dir(dir))
	}

	return nil
}

// snapDataDirs returns the list of data directories for the given snap version
func snapDataDirs(fullName, version string) ([]string, error) {
	// collect the directories, homes first
	found, err := filepath.Glob(filepath.Join(dirs.SnapDataHomeGlob, fullName, version))
	if err != nil {
		return nil, err
	}
	// then system data
	systemPath := filepath.Join(dirs.SnapDataDir, fullName, version)
	found = append(found, systemPath)

	return found, nil
}

// Copy all data for "fullName" from "oldVersion" to "newVersion"
// (but never overwrite)
func copySnapData(fullName, oldVersion, newVersion string) (err error) {
	oldDataDirs, err := snapDataDirs(fullName, oldVersion)
	if err != nil {
		return err
	}

	for _, oldDir := range oldDataDirs {
		// replace the trailing "../$old-ver" with the "../$new-ver"
		newDir := filepath.Join(filepath.Dir(oldDir), newVersion)
		if err := copySnapDataDirectory(oldDir, newDir); err != nil {
			return err
		}
	}

	return nil
}

// Lowlevel copy the snap data (but never override existing data)
func copySnapDataDirectory(oldPath, newPath string) (err error) {
	if _, err := os.Stat(oldPath); err == nil {
		if _, err := os.Stat(newPath); err != nil {
			// there is no golang "CopyFile"
			cmd := exec.Command("cp", "-a", oldPath, newPath)
			if err := cmd.Run(); err != nil {
				if exitCode, err := helpers.ExitCode(err); err == nil {
					return &ErrDataCopyFailed{
						OldPath:  oldPath,
						NewPath:  newPath,
						ExitCode: exitCode}
				}
				return err
			}
		}
	}
	return nil
}
