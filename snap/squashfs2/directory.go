// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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

import "fmt"

func (d *directory) readDirectoryHeader() squashfs_dir_header {
	header := make([]byte, 12)
	d.reader.read(header)

	return squashfs_dir_header{
		count:      readUint32(header[0:]),
		startBlock: readUint32(header[4:]),
		ino:        readUint32(header[8:]),
	}
}

func (d *directory) readDirectoryEntry(header *squashfs_dir_header) (squashfs_dir_entry, int) {
	buffer := make([]byte, 8)
	d.reader.read(buffer)

	entry := squashfs_dir_entry{
		startBlock: header.startBlock,
		offset:     readUint16(buffer[0:]),
		ino:        readInt16(buffer[2:]),
		itype:      readUint16(buffer[4:]),
		size:       readUint16(buffer[6:]),
		name:       "",
	}

	name := make([]byte, entry.size+1)
	d.reader.read(name)
	entry.name = string(name)
	return entry, int(entry.size) + 8 + 1
}

func (d *directory) loadEntries() error {
	d.reader.seek(int64(d.node.startBlock), int(d.node.offset))
	println("loading directory entries", d.node.size)

	if d.node.size == 3 {
		// directory is empty
		return nil
	}

	bytesRead := 0
	for bytesRead < int(d.node.size)-3 {
		dirHeader := d.readDirectoryHeader()
		bytesRead += 12

		println("squashfs: directory header:", dirHeader.count, dirHeader.startBlock, dirHeader.ino)
		if dirHeader.count > directoryMaxEntryCount {
			return fmt.Errorf("squashfs: invalid number of directory entries: %d", dirHeader.count)
		}

		// squashfs is littered with magic arethmetics, count is
		// actually one less than specified in count
		for i := 0; i < int(dirHeader.count)+1; i++ {
			entry, size := d.readDirectoryEntry(&dirHeader)
			println("squashfs: directory entry:", entry.name)
			d.entries = append(d.entries, entry)
			bytesRead += size
		}
	}

	d.loaded = true
	return nil
}

func (d *directory) lookupDirectoryEntry(name string) (*squashfs_dir_entry, error) {
	println("squashfs: looking up:", name)
	if !d.loaded {
		err := d.loadEntries()
		if err != nil {
			return nil, err
		}
	}

	for _, entry := range d.entries {
		if entry.name == name {
			println("squashfs: found entry:", name)
			return &entry, nil
		}
	}
	return nil, fmt.Errorf("squashfs: entry not found: %s", name)
}
