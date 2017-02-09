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

// package build_id allows to read the GNU Build ID note from ELF exectuables.
package build_id

import (
	"debug/elf"
	"encoding/binary"
	"fmt"
	"os"
)

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

// BuildID returns the GNU Build-ID of a given executable
//
// http://fedoraproject.org/wiki/Releases/FeatureBuildId
func GetBuildID(fname string) (BuildID, error) {
	const ELF_NOTE_GNU = "GNU\x00"
	const NT_GNU_BUILD_ID uint32 = 3
	var id BuildID

	// Open the designated ELF file
	f, err := elf.Open(fname)
	if err != nil {
		return id, err
	}
	defer f.Close()

	const specialSectionName = ".note.gnu.build-id"

	// Find the section that holds the build-id note
	section := f.Section(specialSectionName)
	if section == nil {
		return id, fmt.Errorf("cannot find section %q, build with -buildmode=pie", specialSectionName)
	}
	if section.Type != elf.SHT_NOTE {
		return id, fmt.Errorf("section %q has unexpected type (wanted SHT_NOTE, got %s)", section.Name, section.Type)
	}

	// NOTE: this is a ReadSeeker so no need to close it
	sr := section.Open()
	sr.Seek(0, os.SEEK_SET)

	// Read the ELF Note header
	nHdr := new(noteHeader)
	if err := binary.Read(sr, f.ByteOrder, nHdr); err != nil {
		return id, err
	}
	if nHdr.Type != NT_GNU_BUILD_ID {
		return id, fmt.Errorf("note has unexpected type (wanted NT_GNU_BUILD_ID, got %d)", nHdr.Type)
	}

	// Read note name
	noteName := make([]byte, nHdr.Namesz)
	if err := binary.Read(sr, f.ByteOrder, noteName); err != nil {
		return id, err
	}
	if string(noteName) != ELF_NOTE_GNU {
		return id, fmt.Errorf("note has unexpected name (wanted GNU, got %q)", string(noteName))
	}

	noteData := make([]byte, nHdr.Descsz)
	if err := binary.Read(sr, f.ByteOrder, noteData); err != nil {
		return id, err
	}
	return BuildID(noteData), nil
}
