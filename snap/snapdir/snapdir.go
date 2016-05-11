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

package snapdir

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
)

// Snap is the snapdir based snap.
type Snap struct {
	path string
}

// Path returns the path of the backing file.
func (s *Snap) Path() string {
	return s.path
}

// New returns a new Squashfs snap.
func New(path string) *Snap {
	return &Snap{path: path}
}

func (s *Snap) Install(targetPath, mountDir string) error {
	return os.Symlink(s.path, targetPath)
}

func (s *Snap) Unpack(src, dstDir string) error {
	return fmt.Errorf("cannot unpack a snapdir snap")
}

func (s *Snap) ReadFile(file string) (content []byte, err error) {
	return ioutil.ReadFile(filepath.Join(s.path, file))
}
