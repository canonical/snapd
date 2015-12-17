// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package asserts

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/ubuntu-core/snappy/helpers"
)

// utilities to read/write fs entries

func atomicWriteEntry(data []byte, secret bool, top string, subpath ...string) error {
	fpath := filepath.Join(top, filepath.Join(subpath...))
	dir := filepath.Dir(fpath)
	err := os.MkdirAll(dir, 0775)
	if err != nil {
		return err
	}
	fperm := 0664
	if secret {
		fperm = 0600
	}
	return helpers.AtomicWriteFile(fpath, data, os.FileMode(fperm), 0)
}

func readEntry(top string, subpath ...string) ([]byte, error) {
	fpath := filepath.Join(top, filepath.Join(subpath...))
	return ioutil.ReadFile(fpath)
}
