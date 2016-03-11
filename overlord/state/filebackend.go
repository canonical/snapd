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

package state

import (
	"github.com/ubuntu-core/snappy/osutil"
)

type fileBackend struct {
	path string
}

// NewFileBackend creates a new file based state backend
func NewFileBackend(path string) Backend {
	return &fileBackend{path: path}
}

func (sf *fileBackend) Checkpoint(data []byte) error {
	return osutil.AtomicWriteFile(sf.path, data, 0600, 0)
}
