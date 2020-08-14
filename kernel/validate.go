// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package kernel

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

func validateAssetsContent(kernelRoot string, info *Info) error {
	// bare structure content is checked to exist during layout
	// make sure that filesystem content source paths exist as well
	for name, as := range info.Assets {
		for _, c := range as.Content {
			realSource := filepath.Join(kernelRoot, c)
			if !osutil.FileExists(realSource) {
				return fmt.Errorf("content %q: source path %q does not exist", name, c)
			}
			if strings.HasSuffix(c, "/") {
				// expecting a directory
				if !osutil.IsDirectory(realSource + "/") {
					return fmt.Errorf("content %q: %q is not a directory", name, c)
				}
			}
		}
	}
	return nil
}

// Validate checks whether the given directory contains valid kernel snap
// metadata and a matching content.
func Validate(kernelRoot string) error {
	info, err := ReadInfo(kernelRoot)
	if err != nil {
		return fmt.Errorf("invalid kernel metadata: %v", err)
	}

	if err := validateAssetsContent(kernelRoot, info); err != nil {
		return err
	}

	return nil
}
