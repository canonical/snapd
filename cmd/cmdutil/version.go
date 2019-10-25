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
package cmdutil

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

func SnapdVersionFromInfoFile(rootDir string) (string, error) {
	fullInfo := filepath.Join(rootDir, filepath.Join(dirs.CoreLibExecDir, "info"))
	content, err := ioutil.ReadFile(fullInfo)
	if err != nil {
		return "", fmt.Errorf("cannot open snapd info file %q: %s", fullInfo, err)
	}

	if !bytes.HasPrefix(content, []byte("VERSION=")) {
		idx := bytes.Index(content, []byte("\nVERSION="))
		if idx < 0 {
			return "", fmt.Errorf("cannot find snapd version information in %q", content)
		}
		content = content[idx+1:]
	}
	content = content[8:]
	idx := bytes.IndexByte(content, '\n')
	if idx > -1 {
		content = content[:idx]
	}

	return string(content), nil
}
