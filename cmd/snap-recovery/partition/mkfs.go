// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"os/exec"

	"github.com/snapcore/snapd/osutil"
)

func makeFilesystem(node, label, filesystem string) error {
	switch filesystem {
	case "vfat":
		return makeVFATFilesystem(node, label)
	case "ext4":
		return makeExt4Filesystem(node, label)
	default:
		return fmt.Errorf("cannot create unsupported filesystem %q", filesystem)
	}
}

func makeVFATFilesystem(node, label string) error {
	if output, err := exec.Command("mkfs.vfat", "-n", label, node).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}

func makeExt4Filesystem(node, label string) error {
	if output, err := exec.Command("mke2fs", "-t", "ext4", "-L", label, node).CombinedOutput(); err != nil {
		return osutil.OutputErr(output, err)
	}
	return nil
}
