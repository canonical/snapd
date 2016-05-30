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

package overlord

import (
	"time"

	"github.com/snapcore/snapd/osutil"
)

type overlordStateBackend struct {
	path         string
	ensureBefore func(d time.Duration)
}

func (osb *overlordStateBackend) Checkpoint(data []byte) error {
	return osutil.AtomicWriteFile(osb.path, data, 0600, 0)
}

func (osb *overlordStateBackend) EnsureBefore(d time.Duration) {
	osb.ensureBefore(d)
}
