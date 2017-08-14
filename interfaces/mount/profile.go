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

package mount

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// Profile represents an array of mount entries.
type Profile struct {
	Entries []Entry
}

// LoadProfile loads a mount profile from a given file.
//
// The file may be absent, in such case an empty profile is returned without errors.
func LoadProfile(fname string) (*Profile, error) {
	f, err := os.Open(fname)
	if err != nil && os.IsNotExist(err) {
		return &Profile{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadProfile(f)
}

// Save saves a mount profile (fstab-like) to a given file.
// The profile is saved with an atomic write+rename+sync operation.
func (p *Profile) Save(fname string) error {
	var buf bytes.Buffer
	if _, err := p.WriteTo(&buf); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(fname, buf.Bytes(), 0644, osutil.AtomicWriteFlags(0))
}

// ReadProfile reads and parses a mount profile.
//
// The supported format is described by fstab(5).
func ReadProfile(reader io.Reader) (*Profile, error) {
	var p Profile
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		s := scanner.Text()
		if i := strings.IndexByte(s, '#'); i != -1 {
			s = s[0:i]
		}
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		entry, err := ParseEntry(s)
		if err != nil {
			return nil, err
		}
		p.Entries = append(p.Entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &p, nil
}

// WriteTo writes a mount profile to the given writer.
//
// The supported format is described by fstab(5).
// Note that there is no support for comments.
func (p *Profile) WriteTo(writer io.Writer) (int64, error) {
	var written int64
	for i := range p.Entries {
		var n int
		var err error
		if n, err = fmt.Fprintf(writer, "%s\n", p.Entries[i]); err != nil {
			return written, err
		}
		written += int64(n)
	}
	return written, nil
}
