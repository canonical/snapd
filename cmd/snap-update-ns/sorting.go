// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

// byOvernameAndMountPoint allows sorting an array of entries by the
// source of mount entry (whether it's an overname or not) and
// lexically by mount point name.  Automagically adds a trailing slash
// to paths.
type byOvernameAndMountPoint []osutil.MountEntry

func (c byOvernameAndMountPoint) Len() int      { return len(c) }
func (c byOvernameAndMountPoint) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byOvernameAndMountPoint) Less(i, j int) bool {
	iMe := c[i]
	jMe := c[j]

	iOrigin := iMe.XSnapdOrigin()
	jOrigin := jMe.XSnapdOrigin()
	if iOrigin != jOrigin {
		// overname entries should always be sorted first, before
		// entries from layouts or content interface
		if iOrigin == "overname" {
			// overname ith element should be sorted before
			return true
		}
		if jOrigin == "overname" {
			// non-overname ith element should be sorted after
			return false
		}
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

// byOriginAndMountPoint allows sorting an array of entries by the
// source of mount entry (overname, other, layout) and lexically by
// mount point name.  Automagically adds a trailing slash to paths.
type byOriginAndMountPoint []osutil.MountEntry

func (c byOriginAndMountPoint) Len() int      { return len(c) }
func (c byOriginAndMountPoint) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byOriginAndMountPoint) Less(i, j int) bool {
	iMe := c[i]
	jMe := c[j]

	iOrigin := iMe.XSnapdOrigin()
	jOrigin := jMe.XSnapdOrigin()
	if iOrigin != jOrigin {
		// overname entries should always be sorted first, before
		// entries from layouts or content interface
		if iOrigin == "overname" {
			// overname ith element should be sorted before
			return true
		}
		if jOrigin == "overname" {
			// non-overname ith element should be sorted after
			return false
		}
		// neither is overname, can be layout or nothing (implied
		// content)
		if iOrigin == "layout" {
			return false
		}
		// i is not layout, so it must be nothing (implied content)
		if jOrigin == "layout" {
			return true
		}
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
