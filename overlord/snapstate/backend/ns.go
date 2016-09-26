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

package backend

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

func (b Backend) DiscardSnapNamespace(snapName string) error {
	cmd := exec.Command(filepath.Join(dirs.LibExecDir, "snap-discard-ns"), snapName)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cannot discard preserved namespaces of snap %q: %s", snapName, output)
	}
	return nil
}
