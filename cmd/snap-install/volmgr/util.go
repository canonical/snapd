// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package volmgr

import (
	"crypto/rand"
	"os"
)

// wipe overwrites a file with zeros and removes it. It is intended to be used
// only with small files.
func wipe(name string) error {
	// Better solution: have a custom cryptsetup util that reads master key from stdin
	file, err := os.OpenFile(name, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	st, err := file.Stat()
	if err != nil {
		return err
	}

	_, err = file.Write(make([]byte, st.Size()))
	if err != nil {
		return err
	}
	file.Close()

	return os.Remove(name)
}

// createKey creates a random byte slice with the specified size.
func createKey(size int) ([]byte, error) {
	buffer := make([]byte, size)
	_, err := rand.Read(buffer)
	// On return, n == len(b) if and only if err == nil
	return nil, err
}
