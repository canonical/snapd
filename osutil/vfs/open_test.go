// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package vfs_test

import (
	"testing"
	"testing/fstest"

	"github.com/snapcore/snapd/osutil/vfs"
)

func TestVFS_Open(t *testing.T) {
	v := vfs.NewVFS(fstest.MapFS{})
	defer func() {
		p := recover()
		if p != "VFS.Open is not needed" {
			t.Fatal("Unexpected panic", p)
		}
	}()
	_, _ = v.Open("hello.txt")
	panic("VFS.Open should have panicked")
}
