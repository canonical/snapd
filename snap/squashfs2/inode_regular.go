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
	"github.com/snapcore/snapd/snap/squashfs2/internal"
)

func inodeRegularRead(inodeData []byte, reader *metaBlockReader) ([]byte, error) {
	inode := &internal.InodeReg{}
	if err := inode.Parse(inodeData); err != nil {
		return nil, err
	}

	// Read the blocksize table, the blocksizes vary in their meaning
	// based on whether or not the fragment table are used. Currently we
	// have no fragment table support, so we assume the data blocks just mean
	// the sizes of the data blocks, excluding bit 24 that tells us whether or
	// not the block is uncompressed.
	var blockData []byte
	for i := uint32(0); i < inode.Size; {
		data := make([]byte, 4)
		if err := reader.read(data); err != nil {
			return nil, err
		}

		// Add the data to the block data buffer so we can
		// parse it later as well
		blockData = append(blockData, data...)

		// ... but parse it already as we need to know the size
		blockSize := internal.ReadUint32(data)
		i += (blockSize & 0xFEFFFFFF)
	}
	return blockData, nil
}
