// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main

import (
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// byOriginAndMagicDir allows sorting an array of entries that automagically assumes
// each entry ends with a trailing slash.
type byOriginAndMagicDir []osutil.MountEntry

func (c byOriginAndMagicDir) Len() int      { return len(c) }
func (c byOriginAndMagicDir) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byOriginAndMagicDir) Less(i, j int) bool {
	iMe := c[i]
	jMe := c[j]

	iOrigin := iMe.XSnapdOrigin()
	jOrigin := jMe.XSnapdOrigin()
	if iOrigin == "overname" && iOrigin != jOrigin {
		return true
	}

	iDir := c[i].Dir
	jDir := c[j].Dir
	if !strings.HasSuffix(iDir, "/") {
		iDir = iDir + "/"
	}
	if !strings.HasSuffix(jDir, "/") {
		jDir = jDir + "/"
	}
	return iDir < jDir
}
