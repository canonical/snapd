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

package osutil

import (
	"os"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
)

const maxint = int64(^uint(0) >> 1)

var maxcp = maxint // overridden in testing

func doCopyFile(fin, fout fileish, fi os.FileInfo) error {
	size := fi.Size()
	var offset int64
	for offset < size {
		// sendfile is funny; it only copies up to maxint
		// bytes at a time, but takes an int64 offset.
		count := size - offset
		if count > maxcp {
			count = maxcp
		}
		mylog.Check2(syscall.Sendfile(int(fout.Fd()), int(fin.Fd()), &offset, int(count)))

	}

	return nil
}
