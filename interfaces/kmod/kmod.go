// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package kmod

import (
	"fmt"
	"os/exec"

	"github.com/snapcore/snapd/osutil"
)

func LoadModule(module string) error {
	if output, err := exec.Command("modprobe", "--syslog", module).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot load module %s: %s", module, osutil.OutputErr(output, err))
	}
	return nil
}

// loadModules loads given list of modules via modprobe.
// Since different kernels may not have the requested module, we treat any
// error from modprobe as non-fatal and subsequent module loads are attempted
// (otherwise failure to load a module means failure to connect the interface
// and the other security backends)
func loadModules(modules []string) error {
	for _, mod := range modules {
		LoadModule(mod) // ignore errors which are logged by
		// LoadModule() via syslog
	}
	return nil
}
