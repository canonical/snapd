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
	"github.com/snapcore/snapd/osutil"
)

func (b Backend) DiscardSnapNamespace(snapName string) error {
	snapDiscardNs := filepath.Join(dirs.LibExecDir, "snap-discard-ns")
	if osutil.FileExists(snapDiscardNs) {
		// Preserved namespaces need to be discarded only if they are being
		// preserved by snap-confine. If the snap-discard-ns command doesn't
		// exist then the shared mount namespace feature is not available and
		// there is nothing to do here.
		cmd := exec.Command(snapDiscardNs, snapName)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("cannot discard preserved namespaces of snap %q: %s", snapName, output)
		}
	}
	return nil
}
