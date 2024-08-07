// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package maxidmmap

import (
	"fmt"
	"os"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/interfaces/prompting"
	"github.com/snapcore/snapd/logger"
)

const (
	// maxIDFileSize should be enough bytes to encode the maximum prompt ID.
	maxIDFileSize int = 8
)

var (
	ErrMaxIDMmapClosed = fmt.Errorf("cannot compute next ID on max ID mmap which has already been closed")
)

type MaxIDMmap []byte

// OpenMaxIDMmap opens and mmaps the given maxIDFilepath, returning the
// corresponding MaxIDMmap.
//
// If the maxIDFilepath does not exist, or if the file is malformed, it is
// reset to an 8-byte file with a max ID of 0. If the file cannot be created,
// opened, or mmaped, returns an error and a nil MaxIDMmap.
func OpenMaxIDMmap(maxIDFilepath string) (MaxIDMmap, error) {
	maxIDFile, err := os.OpenFile(maxIDFilepath, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, fmt.Errorf("cannot open max ID file: %w", err)
	}
	// The file/FD can be safely closed once the mmap is created. See mmap(2).
	defer maxIDFile.Close()
	fileInfo, err := maxIDFile.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot stat max ID file: %w", err)
	}
	if fileInfo.Size() != int64(maxIDFileSize) {
		if fileInfo.Size() != 0 {
			// Max ID file malformed, best to reset it
			logger.Debugf("max ID file malformed; re-initializing")
		}
		if err = initializeMaxIDFile(maxIDFile); err != nil {
			return nil, err
		}
	}
	conn, err := maxIDFile.SyscallConn()
	if err != nil {
		return nil, fmt.Errorf("cannot get raw file for maxIDFile: %w", err)
	}
	var maxIDMmap []byte
	var controlErr error
	err = conn.Control(func(fd uintptr) {
		// Use Control() so that the file/fd is not garbage collected during
		// the syscall.
		maxIDMmap, controlErr = unix.Mmap(int(fd), 0, maxIDFileSize, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	})
	if err != nil {
		return nil, fmt.Errorf("cannot call control function on maxIDFile conn: %w", err)
	}
	if controlErr != nil {
		return nil, fmt.Errorf("cannot mmap max ID file: %w", controlErr)
	}
	return MaxIDMmap(maxIDMmap), nil
}

// initializeMaxIDFile truncates the given file to maxIDFileSize bytes of zeros.
func initializeMaxIDFile(maxIDFile *os.File) (err error) {
	initial := [maxIDFileSize]byte{}
	if err = maxIDFile.Truncate(int64(len(initial))); err != nil {
		return fmt.Errorf("cannot truncate max ID file: %w", err)
	}
	if _, err = maxIDFile.WriteAt(initial[:], 0); err != nil {
		return fmt.Errorf("cannot initialize max ID file: %w", err)
	}
	return nil
}

// NextID increments the monotonic max ID integer and returns the corresponding
// ID.
//
// The caller must ensure that any relevant lock is held.
func (mim MaxIDMmap) NextID() (prompting.IDType, error) {
	if mim == nil {
		return 0, ErrMaxIDMmapClosed
	}
	// Byte order will be consistent, and want atomic increment
	id := atomic.AddUint64((*uint64)(unsafe.Pointer(&mim[0])), 1)
	return prompting.IDType(id), nil
}

// Close unmaps the underlying byte slice corresponding to the receiving
// max ID mmap, if it has not already been unmapped.
//
// The caller must ensure that any relevant lock is held.
func (mim *MaxIDMmap) Close() error {
	if *mim == nil {
		return nil
	}
	if err := unix.Munmap(*mim); err != nil {
		return err
	}
	*mim = nil
	return nil
}

// IsClosed returns whether the receiving max ID mmap has been unmapped and
// closed, which is indicated by the underlying byte slice being nil.
//
// The caller must ensure that any relevant lock is held.
func (mim *MaxIDMmap) IsClosed() bool {
	return *mim == nil
}
