// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package blockplan

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// memFile is a scratch file that a child process can open by name and that can
// be emptied and used again.
//
// Both properties are needed by the same thing. hdiffz and hpatchz take their
// input and output as paths, not as descriptors, so the source window, the
// patch and the reconstructed plaintext a patch run works on have to be
// nameable; and a delta has as many patch runs as it has changed regions, so
// allocating three files per run would mean thousands of temporary files over
// an apply. Reusing three of them keeps that to three.
//
// The backing is a memfd, which puts the scratch in memory that is freed the
// moment it is closed and leaves nothing on the filesystem to clean up after a
// crash. A kernel without memfd_create gets a plain temporary file instead,
// which behaves the same from the outside.
type memFile struct {
	file   *os.File
	path   string
	onDisk bool
}

// newMemFile creates a scratch file, preferring memory and falling back to disk.
func newMemFile(name string) (*memFile, error) {
	// MFD_CLOEXEC because the descriptor is never meant to be inherited:
	// children are handed the path and open it themselves, and this process
	// runs several of them per patch run.
	fd, err := unix.MemfdCreate(name, unix.MFD_CLOEXEC)
	if err == nil {
		return &memFile{
			file: os.NewFile(uintptr(fd), name),
			// A memfd has no name in any filesystem, so /proc is how a
			// child is given one. The descriptor stays open here for as
			// long as the child needs it.
			path: fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), fd),
		}, nil
	}
	// Fall back on any error rather than on a specific errno: a kernel
	// without memfd_create is the reason this path exists and it reports
	// that as one of several, and a seccomp filter that blocks the call
	// reports something else again.
	return newDiskFile(name)
}

// newDiskFile is newMemFile's fallback: an ordinary temporary file, which Close
// unlinks again. It is a function of its own so that it can be tested
// deliberately -- on every kernel this runs on the fallback is unreachable, and
// an untested fallback is where the bugs live.
func newDiskFile(name string) (*memFile, error) {
	f, err := os.CreateTemp("", fmt.Sprintf("snapd-blockplan-%s-", name))
	if err != nil {
		return nil, fmt.Errorf("cannot create a scratch file in memory or on disk: %w", err)
	}
	return &memFile{file: f, path: f.Name(), onDisk: true}, nil
}

// newMemFileWith creates a scratch file holding data, which is how the
// in-memory halves of a patch run -- a window and a patch the caller already
// has -- are handed to a tool that wants paths.
func newMemFileWith(name string, data []byte) (*memFile, error) {
	m, err := newMemFile(name)
	if err != nil {
		return nil, err
	}
	if _, err := m.file.Write(data); err != nil {
		m.Close()
		return nil, err
	}
	return m, nil
}

// File is the open file, for reading and writing from this process.
func (m *memFile) File() *os.File { return m.file }

// Path names the file for a child process to open.
func (m *memFile) Path() string { return m.path }

// Reset empties the file and rewinds it, ready for the next patch run. The
// truncation is what makes reuse safe: a shorter window written over a longer
// one would otherwise leave the previous run's tail behind it.
func (m *memFile) Reset() error {
	if err := m.file.Truncate(0); err != nil {
		return fmt.Errorf("cannot truncate scratch file: %w", err)
	}
	if _, err := m.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("cannot rewind scratch file: %w", err)
	}
	return nil
}

// Close releases the file, and the memory or disk space behind it.
func (m *memFile) Close() error {
	err := m.file.Close()
	if m.onDisk {
		// The kernel reclaims a memfd on the last close; a temporary file
		// has to be unlinked, and on a device with little room left that
		// is not a detail.
		if rmErr := os.Remove(m.path); rmErr != nil && err == nil {
			err = fmt.Errorf("cannot remove scratch file %s: %w", m.path, rmErr)
		}
	}
	return err
}
