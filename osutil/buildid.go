// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"encoding/hex"
	"errors"
	"os"

	"github.com/ddkwork/golibrary/mylog"
)

var osReadlink = os.Readlink

// ErrNoBuildID is returned when an executable does not contain a Build-ID
var ErrNoBuildID = errors.New("executable does not contain a build ID")

type elfNoteHeader struct {
	Namesz uint32
	Descsz uint32
	Type   uint32
}

const (
	gnuElfNote = "GNU\x00"
	gnuHdrType = 3
	goElfNote  = "Go\x00\x00"
	goHdrType  = 4
)

// ReadBuildID returns the build ID of a given binary. GNU BuildID is is
// preferred over Go BuildID. Returns an error when neither is found.
func ReadBuildID(fname string) (string, error) {
	if buildId := mylog.Check2(readGenericBuildID(fname, gnuElfNote, gnuHdrType)); err == nil {
		return buildId, nil
	}
	return readGenericBuildID(fname, goElfNote, goHdrType)
}

func readGenericBuildID(fname, elfNote string, hdrType uint32) (string, error) {
	// Open the designated ELF file
	f := mylog.Check2(elf.Open(fname))

	defer f.Close()

	for _, section := range f.Sections {

		// We are looking for note sections
		if section.Type != elf.SHT_NOTE {
			continue
		}

		// NOTE: this is a ReadSeeker so no need to close it
		sr := section.Open()
		sr.Seek(0, os.SEEK_SET)

		// Read the ELF Note header
		nHdr := new(elfNoteHeader)
		mylog.Check(binary.Read(sr, f.ByteOrder, nHdr))

		// We are looking for a specific type of note
		if nHdr.Type != hdrType {
			continue
		}

		// Read note name
		noteName := make([]byte, nHdr.Namesz)
		mylog.Check(binary.Read(sr, f.ByteOrder, noteName))

		// We are only interested in GNU build IDs
		if string(noteName) != elfNote {
			continue
		}

		// Read note data
		noteData := make([]byte, nHdr.Descsz)
		mylog.Check(binary.Read(sr, f.ByteOrder, noteData))

		// Return the first build-id we manage to find
		return hex.EncodeToString(noteData), nil
	}
	return "", ErrNoBuildID
}

// MyBuildID return the build-id of the currently running executable
func MyBuildID() (string, error) {
	exe := mylog.Check2(osReadlink("/proc/self/exe"))

	return ReadBuildID(exe)
}
