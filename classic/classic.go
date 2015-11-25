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

package classic

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
)

// Enabled returns true if the classic mode is already enabled
func Enabled() bool {
	return helpers.FileExists(filepath.Join(dirs.ClassicDir, "etc", "apt", "sources.list"))
}

// Run runs a shell in the classic environment
func Run() error {
	fmt.Println("Entering classic dimension")
	cmd := exec.Command("/usr/share/ubuntu-snappy-cli/snappy-classic.sh")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Create creates a new classic shell envirionment
func Create() error {
	cmd := exec.Command("/usr/share/ubuntu-snappy-cli/snappy-setup-classic.sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Destroy destroys a classic environment
func Destroy() error {
	return fmt.Errorf("no implemented yet, need to undo bind mounts")
	/*
		cmd := exec.Command("rm", "-rf", dirs.ClassicDir)
		return cmd.Run()
	*/
}
