// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"bytes"
	"io"
)

type mountInfoMockFile struct{ *bytes.Buffer }

func (f *mountInfoMockFile) Close() error {
	return nil
}

func MockMountInfo(content string) (restore func()) {
	old := openMountInfoFile
	openMountInfoFile = func() (io.ReadCloser, error) {
		return &mountInfoMockFile{bytes.NewBufferString(content)}, nil
	}
	return func() {
		openMountInfoFile = old
	}
}

func MockFindUid(f func(string) (uint64, error)) (restore func()) {
	old := FindUid
	FindUid = f
	return func() {
		FindUid = old
	}
}

func MockFindGid(f func(string) (uint64, error)) (restore func()) {
	old := FindGid
	FindGid = f
	return func() {
		FindGid = old
	}
}
