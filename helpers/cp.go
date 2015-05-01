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

type CopyFlag uint8

const (
	CopyFlagDefault CopyFlag = 0
	CopyFlagSync    CopyFlag = 1 << iota
)

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

	fout, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("unable to create %s: %v", dst, err)
	}
	defer func() {
		if cerr := fout.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("when closing %s: %v", dst, cerr)
		}
	}()

	if err := doCopyFile(fin, fout); err != nil {
		fmt.Errorf("unable to copy %s to %s: %v", src, dst, err)
	}

	if flags&CopyFlagSync != 0 {
		if err = fout.Sync(); err != nil {
			return fmt.Errorf("when syncing %s: %v", dst, err)
		}
	}

	return nil
}
