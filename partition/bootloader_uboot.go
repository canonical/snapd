// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package partition

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"launchpad.net/snappy/helpers"

	"github.com/mvo5/goconfigparser"
	"github.com/mvo5/uboot-go/uenv"
)

const (
	bootloaderUbootDirReal        = "/boot/uboot"
	bootloaderUbootConfigFileReal = "/boot/uboot/uEnv.txt"

	// File created by u-boot itself when
	// bootloaderBootmodeTry == "try" which the
	// successfully booted system must remove to flag to u-boot that
	// this partition is "good".
	bootloaderUbootStampFileReal = "/boot/uboot/snappy-stamp.txt"

	// DEPRECATED:
	bootloaderUbootEnvFileReal = "/boot/uboot/snappy-system.txt"

	// the real uboot env
	bootloaderUbootFwEnvFileReal = "/boot/uboot/uboot.env"
)

// var to make it testable
var (
	bootloaderUbootDir        = bootloaderUbootDirReal
	bootloaderUbootConfigFile = bootloaderUbootConfigFileReal
	bootloaderUbootStampFile  = bootloaderUbootStampFileReal
	bootloaderUbootEnvFile    = bootloaderUbootEnvFileReal
	bootloaderUbootFwEnvFile  = bootloaderUbootFwEnvFileReal
	atomicFileUpdate          = atomicFileUpdateImpl
)

const bootloaderNameUboot bootloaderName = "u-boot"

type uboot struct {
	bootloaderType
}

// Stores a Name and a Value to be added as a name=value pair in a file.
// TODO convert to map
type configFileChange struct {
	Name  string
	Value string
}

var setBootVar = func(name, value string) error { return nil }
var getBootVar = func(name string) (string, error) { return "", nil }

// newUboot create a new Uboot bootloader object
func newUboot(partition *Partition) bootLoader {
	if !helpers.FileExists(bootloaderUbootConfigFile) {
		return nil
	}

	b := newBootLoader(partition, bootloaderUbootDir)
	if b == nil {
		return nil
	}
	u := uboot{bootloaderType: *b}

	if helpers.FileExists(bootloaderUbootFwEnvFile) {
		setBootVar = setBootVarFwEnv
		getBootVar = getBootVarFwEnv
	} else {
		setBootVar = setBootVarLegacy
		getBootVar = getBootVarLegacy
	}

	return &u
}

func (u *uboot) Name() bootloaderName {
	return bootloaderNameUboot
}

func (u *uboot) ToggleRootFS(otherRootfs string) (err error) {
	if err := setBootVar(bootloaderRootfsVar, string(otherRootfs)); err != nil {
		return err
	}

	return setBootVar(bootloaderBootmodeVar, bootloaderBootmodeTry)
}

func getBootVarLegacy(name string) (value string, err error) {
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadFile(bootloaderUbootEnvFile); err != nil {
		return "", nil
	}

	return cfg.Get("", name)
}

func setBootVarLegacy(name, value string) error {
	curVal, err := getBootVarLegacy(name)
	if err == nil && curVal == value {
		return nil
	}

	changes := []configFileChange{
		configFileChange{
			Name:  name,
			Value: value,
		},
	}

	return modifyNameValueFile(bootloaderUbootEnvFile, changes)
}

func setBootVarFwEnv(name, value string) error {
	env, err := uenv.Open(bootloaderUbootFwEnvFile)
	if err != nil {
		return err
	}

	// already set, nothing to do
	if env.Get(name) == value {
		return nil
	}

	env.Set(name, value)
	return env.Save()
}

func getBootVarFwEnv(name string) (string, error) {
	env, err := uenv.Open(bootloaderUbootFwEnvFile)
	if err != nil {
		return "", err
	}

	return env.Get(name), nil
}

func (u *uboot) GetBootVar(name string) (value string, err error) {
	return getBootVar(name)
}

func (u *uboot) GetNextBootRootFSName() (label string, err error) {
	value, err := u.GetBootVar(bootloaderRootfsVar)
	if err != nil {
		// should never happen
		return "", err
	}

	return value, nil
}

// FIXME: this is super similar to grub now, refactor to extract the
//        common code
func (u *uboot) MarkCurrentBootSuccessful(currentRootfs string) error {
	// Clear the variable set on boot to denote a good boot.
	if err := setBootVar(bootloaderTrialBootVar, "0"); err != nil {
		return err
	}

	if err := setBootVar(bootloaderRootfsVar, currentRootfs); err != nil {
		return err
	}

	if err := setBootVar(bootloaderBootmodeVar, bootloaderBootmodeSuccess); err != nil {
		return err
	}

	// legacy support, does not error if the file is not there
	return os.RemoveAll(bootloaderUbootStampFile)
}

func (u *uboot) BootDir() string {
	return bootloaderUbootDir
}

// Write lines to file atomically. File does not have to preexist.
// FIXME: put into utils package
func atomicFileUpdateImpl(file string, lines []string) (err error) {
	tmpFile := fmt.Sprintf("%s.NEW", file)

	// XXX: if go switches to use aio_fsync, we need to open the dir for writing
	dir, err := os.Open(filepath.Dir(file))
	if err != nil {
		return err
	}
	defer dir.Close()

	if err := writeLines(lines, tmpFile); err != nil {
		return err
	}

	// atomic update
	if err := os.Rename(tmpFile, file); err != nil {
		return err
	}

	return dir.Sync()
}

// Rewrite the specified file, applying the specified set of changes.
// Lines not in the changes slice are left alone.
// If the original file does not contain any of the name entries (from
// the corresponding configFileChange objects), those entries are
// appended to the file.
//
// FIXME: put into utils package
// FIXME: improve logic
func modifyNameValueFile(file string, changes []configFileChange) (err error) {
	var updated []configFileChange

	lines, err := readLines(file)
	if err != nil {
		return err
	}

	var new []string
	// we won't write to a file if we don't need to.
	updateNeeded := false

	for _, line := range lines {
		for _, change := range changes {
			if strings.HasPrefix(line, fmt.Sprintf("%s=", change.Name)) {
				value := strings.SplitN(line, "=", 2)[1]
				// updated is used later to see if you had the originally requested
				// value.
				updated = append(updated, change)
				if value != change.Value {
					line = fmt.Sprintf("%s=%s", change.Name, change.Value)
					updateNeeded = true
				}
			}
		}
		new = append(new, line)
	}

	lines = new

	for _, change := range changes {
		got := false
		for _, update := range updated {
			if update.Name == change.Name {
				got = true
				break
			}
		}

		if !got {
			updateNeeded = true

			// name/value pair did not exist in original
			// file, so append
			lines = append(lines, fmt.Sprintf("%s=%s",
				change.Name, change.Value))
		}
	}

	if updateNeeded {
		return atomicFileUpdate(file, lines)
	}

	return nil
}

// FIXME: put into utils package
func readLines(path string) (lines []string, err error) {

	file, err := os.Open(path)

	if err != nil {
		return nil, err
	}

	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	return lines, scanner.Err()
}

// FIXME: put into utils package
func writeLines(lines []string, path string) (err error) {

	file, err := os.Create(path)

	if err != nil {
		return err
	}

	defer func() {
		e := file.Close()
		if err == nil {
			err = e
		}
	}()

	writer := bufio.NewWriter(file)

	for _, line := range lines {
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return err
		}
	}

	if err := writer.Flush(); err != nil {
		return err
	}

	return file.Sync()
}
