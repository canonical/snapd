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

package internal

import (
	"os"
)

// SizedFile holds an os.File plus its (initial) size.
type SizedFile struct {
	*os.File
	size int64
}

func NewSizedFile(f *os.File) (*SizedFile, error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return &SizedFile{File: f, size: fi.Size()}, nil
}

func (f *SizedFile) Size() int64 {
	return f.size
}
