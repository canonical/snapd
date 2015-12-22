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

package pkg

// File is the interface to interact with the low-level snap files
type File interface {
	Verify(allowUnauthenticated bool) error
	Close() error
	UnpackWithDropPrivs(targetDir, rootDir string) error
	MetaMember(name string) ([]byte, error)
	ExtractHashes(targetDir string) error
	//Unpack unpacks the src parts to the dst directory
	Unpack(src, dst string) error

	// NeedsMountUnit determines whether it's required to setup
	// a mount unit for the snap when the snap is installed
	NeedsMountUnit() bool
}
