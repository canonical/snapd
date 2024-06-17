// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package fips

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
)

// IsEnabled returns true when the OS reports that FIPS mode is enabled.
// Otherwise returns false.
func IsEnabled() (bool, error) {
	p := filepath.Join(dirs.GlobalRootDir, "/proc/sys/crypto/fips_enabled")

	f, err := os.Open(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()

	// see: https://elixir.bootlin.com/linux/v6.9.4/source/crypto/fips.c#L46
	// the file content is '[0|1]\n'
	var buf [2]byte
	n, err := f.Read(buf[:])
	if err != nil {
		return false, err
	}
	return bytes.Equal(buf[:n], []byte("1\n")), nil
}
