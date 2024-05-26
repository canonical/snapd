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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

func validateAssetsContent(kernelRoot string, info *Info) error {
	// bare structure content is checked to exist during layout
	// make sure that filesystem content source paths exist as well
	for name, as := range info.Assets {
		for _, assetContent := range as.Content {
			c := assetContent
			// a single trailing / is allowed and indicates a directory
			isDir := strings.HasSuffix(c, "/")
			if isDir {
				c = strings.TrimSuffix(c, "/")
			}
			if filepath.Clean(c) != c || strings.Contains(c, "..") || c == "/" {
				return fmt.Errorf("asset %q: invalid content %q", name, assetContent)
			}
			realSource := filepath.Join(kernelRoot, c)
			if !osutil.FileExists(realSource) {
				return fmt.Errorf("asset %q: content %q source path does not exist", name, assetContent)
			}
			if isDir {
				// expecting a directory
				if !osutil.IsDirectory(realSource + "/") {
					return fmt.Errorf("asset %q: content %q is not a directory", name, assetContent)
				}
			}
		}
	}
	return nil
}

// Validate checks whether the given directory contains valid kernel snap
// metadata and a matching content.
func Validate(kernelRoot string) error {
	info := mylog.Check2(ReadInfo(kernelRoot))
	mylog.Check(validateAssetsContent(kernelRoot, info))

	return nil
}
