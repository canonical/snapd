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

package helpers

import (
	"fmt"
	"os"
)

// CopyFlag is used to tweak the behaviour of CopyFile
type CopyFlag uint8

const (
	// CopyFlagDefault is the default behaviour
	CopyFlagDefault CopyFlag = 0
	// CopyFlagSync does a sync after copying the files
	CopyFlagSync CopyFlag = 1 << iota
)

// CopyFile copies src to dst
func CopyFile(src, dst string, flags CopyFlag) (err error) {
	fin, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("unable to open %s: %v", src, err)
	}
	defer func() {
		if cerr := fin.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("when closing %s: %v", src, cerr)
		}
	}()

	fi, err := fin.Stat()
	if err != nil {
		return fmt.Errorf("unable to stat %s: %v", src, err)
	}

	fout, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode())
	if err != nil {
		return fmt.Errorf("unable to create %s: %v", dst, err)
	}
	defer func() {
		if cerr := fout.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("when closing %s: %v", dst, cerr)
		}
	}()

	if err := doCopyFile(fin, fout, fi); err != nil {
		fmt.Errorf("unable to copy %s to %s: %v", src, dst, err)
	}

	if flags&CopyFlagSync != 0 {
		if err = fout.Sync(); err != nil {
			return fmt.Errorf("when syncing %s: %v", dst, err)
		}
	}

	return nil
}
