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

package backend

import (
	"os"
	"os/user"
)

var (
	AddToZip = addToZip
)

func MockUserLookup(newLookup func(string) (*user.User, error)) func() {
	oldLookup := userLookup
	userLookup = newLookup
	return func() {
		userLookup = oldLookup
	}
}

func MockUserLookupId(newLookupId func(string) (*user.User, error)) func() {
	oldLookupId := userLookupId
	userLookupId = newLookupId
	return func() {
		userLookupId = oldLookupId
	}
}

func MockDirOpen(newDirOpen func(string) (*os.File, error)) func() {
	oldDirOpen := dirOpen
	dirOpen = newDirOpen
	return func() {
		dirOpen = oldDirOpen
	}
}

func MockDirNames(newDirNames func(*os.File, int) ([]string, error)) func() {
	oldDirNames := dirNames
	dirNames = newDirNames
	return func() {
		dirNames = oldDirNames
	}
}

func MockOpen(newOpen func(string) (*Reader, error)) func() {
	oldOpen := open
	open = newOpen
	return func() {
		open = oldOpen
	}
}
