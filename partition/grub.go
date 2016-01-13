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
	"fmt"
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"

	"github.com/mvo5/goconfigparser"
)

// var to make it testable
var (
	grubEnvCmd = "/usr/bin/grub-editenv"
)

func grubDir() string {
	return filepath.Join(dirs.GlobalRootDir, "/boot/grub")
}
func grubConfigFile() string {
	return filepath.Join(grubDir(), "grub.cfg")
}
func grubEnvFile() string {
	return filepath.Join(grubDir(), "grubenv")
}

type grub struct {
}

// newGrub create a new Grub bootloader object
func newGrub() bootLoader {
	if !helpers.FileExists(grubConfigFile()) {
		return nil
	}

	return &grub{}
}

func (g *grub) GetBootVar(name string) (value string, err error) {
	// Grub doesn't provide a get verb, so retrieve all values and
	// search for the required variable ourselves.
	output, err := runCommandWithStdout(grubEnvCmd, grubEnvFile(), "list")
	if err != nil {
		return "", err
	}

	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadString(output); err != nil {
		return "", err
	}

	return cfg.Get("", name)
}

func (g *grub) SetBootVar(name, value string) (err error) {
	// note that strings are not quoted since because
	// RunCommand() does not use a shell and thus adding quotes
	// stores them in the environment file (which is not desirable)
	arg := fmt.Sprintf("%s=%s", name, value)
	return runCommand(grubEnvCmd, grubEnvFile(), "set", arg)
}
