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

package partition

import (
	"path/filepath"
)

// The full path to the cache directory, which is used as a
// scratch pad, for downloading new images to and bind mounting the
// rootfs.
const cacheDirReal = "/writable/cache"

var (
	// useful for overwriting in the tests
	cacheDir = cacheDirReal

	// Directory to mount writable root filesystem below the cache
	// diretory.
	mountTargetReal = filepath.Join(cacheDir, "system")

	// useful to override in tests
	mountTarget = mountTargetReal
)
