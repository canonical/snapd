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

/*
#include <fcntl.h> // for AT_*
#include <sys/stat.h>
#include <stdlib.h> // for free()

int touch(const char *pathname) {
    // the first 0 is the dirfd, which is ignored if pathname is absolute
    // the second 0 is a null pointer => use current time
    return utimensat(0, pathname, 0, AT_SYMLINK_NOFOLLOW);
}
*/
import "C"

import (
	"errors"
	"path/filepath"
	"unsafe"
)

var ErrNotAbsPath = errors.New("not an absolute path")

// UpdateTimestamp updates the timestamp of the file at pathname. It does not
// create it if it does not exist. It does not dereference it if it is a
// symlink. It's like `touch -c -h pathname`.
//
// pathname must be absolute.
func UpdateTimestamp(pathname string) error {
	if !filepath.IsAbs(pathname) {
		return ErrNotAbsPath
	}

	p := C.CString(pathname)
	defer C.free(unsafe.Pointer(p))

	_, e := C.touch(p)

	return e
}
