// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// ErrNoBuildID is returned when an executable does not contain a Build-ID
var ErrNoBuildID = errors.New("executable does not contain GNU Build-Id")

// BuildID is an array of bytes that identify a given build of an executable.
type BuildID []byte

// String returns the familiar representation, namely "BuildID[...]=..."
func (id BuildID) String() string {
	switch len(id) {
	case 0x14: // SHA1 note:
		return fmt.Sprintf("BuildID[sha1]=%x", []byte(id))
	default:
		return fmt.Sprintf("BuildID[???]=%x", []byte(id))
	}
}

// ELF Note header.
type noteHeader struct {
	Namesz uint32
	Descsz uint32
	Type   uint32
}

// GetBuildID returns the GNU Build-ID of a given executable
//
// http://fedoraproject.org/wiki/Releases/FeatureBuildId
func GetBuildID(fname string) (*BuildID, error) {
	const ELF_NOTE_GNU = "GNU\x00"
	const NT_GNU_BUILD_ID uint32 = 3

	// Open the designated ELF file
	f, err := elf.Open(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	const specialSectionName = ".note.gnu.build-id"

	for _, section := range f.Sections {

		// We are looking for note sections
		if section.Type != elf.SHT_NOTE {
			continue
		}

		// NOTE: this is a ReadSeeker so no need to close it
		sr := section.Open()
		sr.Seek(0, os.SEEK_SET)

		// Read the ELF Note header
		nHdr := new(noteHeader)
		if err := binary.Read(sr, f.ByteOrder, nHdr); err != nil {
			return nil, err
		}

		// We are looking for a specific type of note
		if nHdr.Type != NT_GNU_BUILD_ID {
			continue
		}

		// Read note name
		noteName := make([]byte, nHdr.Namesz)
		if err := binary.Read(sr, f.ByteOrder, noteName); err != nil {
			return nil, err
		}

		// We are only interested in GNU build IDs
		if string(noteName) != ELF_NOTE_GNU {
			continue
		}

		// Read note data
		noteData := make([]byte, nHdr.Descsz)
		if err := binary.Read(sr, f.ByteOrder, noteData); err != nil {
			return nil, err
		}

		// Return the first build-id we manage to find
		id := BuildID(noteData)
		return &id, nil
	}
	return nil, ErrNoBuildID
}
