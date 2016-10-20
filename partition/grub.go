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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"

	"github.com/mvo5/goconfigparser"
)

// var to make it testable
var (
	grubEnvCmd = "/usr/bin/grub-editenv"
)

type grub struct {
}

// newGrub create a new Grub bootloader object
func newGrub() Bootloader {
	g := &grub{}
	if !osutil.FileExists(g.ConfigFile()) {
		return nil
	}

	return g
}

func (g *grub) Name() string {
	return "grub"
}

func (g *grub) Dir() string {
	return filepath.Join(dirs.GlobalRootDir, "/boot/grub")
}

func (g *grub) ConfigFile() string {
	return filepath.Join(g.Dir(), "grub.cfg")
}

func (g *grub) envFile() string {
	return filepath.Join(g.Dir(), "grubenv")
}

func (g *grub) GetBootVars(names ...string) (map[string]string, error) {
	out := map[string]string{}

	// Grub doesn't provide a get verb, so retrieve all values and
	// search for the required variable ourselves.
	output, err := runCommand(grubEnvCmd, g.envFile(), "list")
	if err != nil {
		return nil, err
	}

	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	if err := cfg.ReadString(output); err != nil {
		return nil, err
	}

	for _, name := range names {
		v, err := cfg.Get("", name)
		if err != nil {
			return nil, err
		}
		out[name] = v
	}

	return out, nil
}

func (g *grub) SetBootVars(values map[string]string) error {
	// note that strings are not quoted since because
	// runCommand does not use a shell and thus adding quotes
	// stores them in the environment file (which is not desirable)
	args := []string{grubEnvCmd, g.envFile(), "set"}
	for k, v := range values {
		args = append(args, fmt.Sprintf("%s=%s", k, v))
	}
	_, err := runCommand(args...)
	return err
}
