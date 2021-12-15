// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
 * https://www.kernel.org/doc/html/v5.8/filesystems/squashfs.html
 */

package squashfs2

import (
	"fmt"

	"github.com/snapcore/snapd/snap/squashfs2/internal"
)

const (
	// directories when empty has the size 3 to include virtual entries
	// like '.' and '..'
	directoryEmptySize = 3

	directoryHeaderSize = 12
	directoryEntrySize  = 8
)

func (d *directory) readHeader() (internal.DirectoryHeader, error) {
	data := make([]byte, directoryHeaderSize)
	if err := d.reader.read(data); err != nil {
		return internal.DirectoryHeader{}, err
	}

	header := internal.DirectoryHeader{}
	if err := header.Parse(data); err != nil {
		return internal.DirectoryHeader{}, err
	}
	return header, nil
}

func (d *directory) readEntry(header *internal.DirectoryHeader) (internal.DirectoryEntry, int, error) {
	buffer := make([]byte, directoryEntrySize)
	if err := d.reader.read(buffer); err != nil {
		return internal.DirectoryEntry{}, 0, err
	}

	// the parser does not parse the name, so we have to do it here, as
	// we need to know the size of the name before reading it
	entry := internal.DirectoryEntry{}
	if err := entry.Parse(buffer); err != nil {
		return internal.DirectoryEntry{}, 0, err
	}

	name := make([]byte, entry.Size+1)
	if err := d.reader.read(name); err != nil {
		return internal.DirectoryEntry{}, 0, err
	}

	entry.StartBlock = header.StartBlock
	entry.Name = string(name)

	// We've read the name length, 8 bytes for the directory entry
	// and 1 extra byte for the null terminator
	bytesRead := int(entry.Size) + directoryEntrySize + 1
	return entry, bytesRead, nil
}

func (d *directory) loadEntries() error {
	if d.node.Size == directoryEmptySize {
		// directory is empty
		return nil
	}

	if err := d.reader.seek(int64(d.node.StartBlock), int(d.node.Offset)); err != nil {
		return err
	}

	bytesRead := 0
	for bytesRead < int(d.node.Size)-directoryEmptySize {
		dirHeader, err := d.readHeader()
		if err != nil {
			return err
		}

		bytesRead += directoryHeaderSize

		if dirHeader.Count > directoryMaxEntryCount {
			return fmt.Errorf("squashfs: invalid number of directory entries: %d", dirHeader.Count)
		}

		// squashfs is littered with magic arethmetics, count is
		// actually one less than specified in count
		for i := 0; i < int(dirHeader.Count)+1; i++ {
			entry, size, err := d.readEntry(&dirHeader)
			if err != nil {
				return err
			}

			d.entries = append(d.entries, entry)
			bytesRead += size
		}
	}

	d.loaded = true
	return nil
}

func (d *directory) lookupDirectoryEntry(name string) (*internal.DirectoryEntry, error) {
	if !d.loaded {
		err := d.loadEntries()
		if err != nil {
			return nil, err
		}
	}

	for _, entry := range d.entries {
		if entry.Name == name {
			return &entry, nil
		}
	}
	return nil, fmt.Errorf("squashfs: entry not found: %s", name)
}
