// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package backend

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd/fdstore"
)

var (
	// ErrInsufficientCapacity is returned by SecretState.Set when the serialized
	// state does not fit in the fixed-size backing store.
	ErrInsufficientCapacity = errors.New("insufficient capacity in secret state")
)

var (
	fdstoreAdd = fdstore.Add
	fdstoreGet = fdstore.Get

	unixMemfdSecret = unix.MemfdSecret
	unixMemfdCreate = unix.MemfdCreate
	unixMmap        = unix.Mmap
	unixMunmap      = unix.Munmap
)

const (
	secretStateSize        = 1024 * 8 // 8KB
	secretStateHeaderMagic = uint32(0x04081999)

	// The first 32 bytes of the file are reserved for the header, the secret data starts at offset 32.
	secretStateHeaderSize = 32
)

// SecretState is an interface for storing secrets than cannot be persisted
// to disk.
//
// SecretState piggybacks on the main state.State lock: the caller must
// hold the state lock while calling Get, Has, Set and Close.
type SecretState interface {
	// Get unmarshals the stored value associated with the provided key
	// into the value parameter.
	// It returns state.ErrNoState if there is no entry for key.
	Get(key string, value any) error

	// Has returns whether the provided key has an associated value.
	Has(key string) bool

	// Set associates value with key for future consulting by managers.
	// The provided value must properly marshal and unmarshal with encoding/json.
	// It returns ErrInsufficientCapacity if the serialized state does not
	// fit in the fixed-size backing store.
	Set(key string, value any) error

	// Close releases the resources associated with the secret state.
	// After Close is called the state can no longer be used.
	Close() error
}

type secretStateHeader struct {
	magic   uint32
	version uint16
	size    uint64
}

func initSecretStateHeader(b []byte) secretStateHeader {
	header := secretStateHeader{
		magic: binary.LittleEndian.Uint32(b[0:4]),
	}
	if header.magic != secretStateHeaderMagic {
		// unknown magic, wipe the whole file to avoid leaking
		// secrets from previous usage.
		// TODO:GOVERSION: use clear() once we are on go>=1.21
		for i := range b {
			b[i] = 0
		}

		// initialize header
		header = secretStateHeader{
			magic:   secretStateHeaderMagic,
			version: 1,
			size:    0,
		}
		header.writeTo(b[:secretStateHeaderSize])
	} else {
		header.version = binary.LittleEndian.Uint16(b[4:6])
		header.size = binary.LittleEndian.Uint64(b[6:14])
	}
	return header
}

func (h *secretStateHeader) writeTo(b []byte) {
	binary.LittleEndian.PutUint32(b[0:4], h.magic)
	binary.LittleEndian.PutUint16(b[4:6], h.version)
	binary.LittleEndian.PutUint64(b[6:14], h.size)
}

type customData map[string]*json.RawMessage

func (data customData) get(key string, value any) error {
	entryJSON := data[key]
	if entryJSON == nil {
		return &state.NoStateError{Key: key}
	}
	err := json.Unmarshal(*entryJSON, value)
	if err != nil {
		return fmt.Errorf("internal error: could not unmarshal state entry %q: %v", key, err)
	}
	return nil
}

func (data customData) has(key string) bool {
	return data[key] != nil
}

func (data customData) set(key string, value any) {
	if value == nil {
		delete(data, key)
		return
	}
	serialized, err := json.Marshal(value)
	if err != nil {
		logger.Panicf("internal error: could not marshal value for state entry %q: %v", key, err)
	}
	entryJSON := json.RawMessage(serialized)
	data[key] = &entryJSON
}

type stateLockChecker interface {
	// EnsureLocked panics if the state lock is not held.
	EnsureLocked()
}

type secretState struct {
	f      *os.File
	data   customData
	header secretStateHeader
	mmap   []byte

	closed bool
	// TODO: once state.State exposes an exported method to check that the state
	// lock is held (e.g. state.EnsureLocked), use it instead.
	stateChecker stateLockChecker
}

func (s *secretState) ensureLocked() {
	if s == nil {
		return
	}
	if s.stateChecker == nil {
		logger.Panicf("internal error: secret state has no associated state lock checker")
	}

	s.stateChecker.EnsureLocked() // ensure the state lock is held, panic if not
}

func (s *secretState) Get(key string, value any) error {
	s.ensureLocked()
	if s.closed {
		return fmt.Errorf("internal error: attempt to get key %q from closed state", key)
	}

	return s.data.get(key, value)
}

func (s *secretState) Has(key string) bool {
	s.ensureLocked()
	if s.closed {
		return false
	}

	return s.data.has(key)
}

func (s *secretState) Set(key string, value any) error {
	s.ensureLocked()
	if s.closed {
		return fmt.Errorf("internal error: attempt to set key %q on closed state", key)
	}

	// remember the previous entry so the change can be reverted if the
	// resulting state does not fit in the fixed-size backing store.
	prevValue, hadPrev := s.data[key]

	s.data.set(key, value)
	p, err := json.Marshal(s.data)
	if err != nil {
		return fmt.Errorf("internal error: cannot marshal state data: %v", err)
	}

	needed := uint64(len(p))
	if s.capacity() < needed {
		// revert the change since the new state does not fit
		if hadPrev {
			s.data[key] = prevValue
		} else {
			delete(s.data, key)
		}
		return fmt.Errorf("cannot set key %q: %w", key, ErrInsufficientCapacity)
	}

	prevSize := s.header.size
	copy(s.mmap[secretStateHeaderSize:], p)
	// If the serialized blob shrank, wipe the now-unused tail so old secrets
	// don't remain readable in the backing memfd.
	if prevSize > needed {
		prev := s.mmap[secretStateHeaderSize+needed : secretStateHeaderSize+prevSize]
		// TODO:GOVERSION: use clear() once we are on go>=1.21
		for i := range prev {
			prev[i] = 0
		}
	}
	s.header.size = uint64(needed)
	s.header.writeTo(s.mmap[:secretStateHeaderSize])
	return nil
}

func (s *secretState) capacity() uint64 {
	l := uint64(len(s.mmap))
	if l >= secretStateHeaderSize {
		return l - secretStateHeaderSize
	}
	// unlikely
	return 0
}

func (s *secretState) Close() error {
	s.ensureLocked()
	return s.closeLocked()
}

func (s *secretState) closeLocked() error {
	if s == nil || s.closed {
		return nil
	}

	s.closed = true

	var errs []error
	if s.mmap != nil {
		if err := unixMunmap(s.mmap); err != nil {
			errs = append(errs, err)
		}
		s.mmap = nil
	}
	if s.f != nil {
		if err := s.f.Close(); err != nil {
			errs = append(errs, err)
		}
		s.f = nil
	}

	return strutil.JoinErrors(errs...)
}

func openSecretStateFile() (*os.File, error) {
	f, err := fdstoreGet(fdstore.FdNameMemfdSecretState)
	if errors.Is(err, fdstore.ErrNotFound) || errors.Is(err, fdstore.ErrUnsupportedSystemdVersion) {
		fdstoreSupported := !errors.Is(err, fdstore.ErrUnsupportedSystemdVersion)
		fd, err := unixMemfdSecret(0)
		if err != nil {
			// fallback to memfd-create if memfd-secret is not supported
			logger.Debugf("cannot create memfd-secret (%v), falling back to memfd-create", err)
			fd, err = unixMemfdCreate("secret-state", 0)
			if err != nil {
				return nil, fmt.Errorf("cannot create secret state file: %w", err)
			}
		}
		// TODO: Use raw fcntl and check for errors.
		unix.CloseOnExec(fd)

		f = os.NewFile(uintptr(fd), "secret-state")
		// memfd-secret files are created with size 0, so we need
		// to truncate it to the desired size.
		if err := f.Truncate(secretStateSize); err != nil {
			return nil, fmt.Errorf("cannot truncate secret state file: %w", err)
		}

		if fdstoreSupported {
			// only add to the fdstore if systemd supports it. If the systemd
			// version is too old, we will just use the memfd without adding
			// it to the fdstore, persistence across snapd restarts will be
			// lost but it is better than crashing.
			if err := fdstoreAdd(fdstore.FdNameMemfdSecretState, f); err != nil {
				return nil, fmt.Errorf("cannot add secret state to fdstore: %w", err)
			}
		} else {
			logger.Debugf("secret state will not persist across snapd restarts: systemd version too old to support fdstore")
		}
	} else if err != nil {
		return nil, fmt.Errorf("cannot get secret state from fdstore: %w", err)
	}
	return f, nil
}

// OpenSecretState returns the memfd-secret backed state used to store
// secrets that can persist through snapd restarts. If memfd-secret is
// not supported, it falls back to using memfd-create. SecretState
// uses passed stateChecker to ensure that the caller holds the state
// lock while accessing the secret state.
//
// Note that only a single instance of the secret state should be opened
// at a time.
func OpenSecretState(stateChecker stateLockChecker) (retState SecretState, retErr error) {
	f, err := openSecretStateFile()
	if err != nil {
		return nil, fmt.Errorf("cannot open secret state file: %w", err)
	}
	defer func() {
		if retErr != nil {
			f.Close()
		}
	}()

	finfo, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot stat memfd-secret state: %w", err)
	}

	if finfo.Size() < secretStateHeaderSize {
		// XXX: file does not even fit the header, consider removing the file
		// and creating a new one with the correct size.
		return nil, fmt.Errorf("secret state file size %d is too small", finfo.Size())
	}

	size := finfo.Size()
	if size > math.MaxInt {
		return nil, fmt.Errorf("cannot mmap memfd-secret state: memfd-secret state size too large: %d", size)
	}
	mmap, err := unixMmap(int(f.Fd()), 0, int(finfo.Size()), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("cannot mmap memfd-secret state: %w", err)
	}
	defer func() {
		if retErr != nil {
			// cleanup mmapped memory on error
			if err := unixMunmap(mmap); err != nil {
				logger.Noticef("cannot unmap memfd-secret state: %v", err)
			}
		}
	}()

	s := &secretState{
		f:            f,
		stateChecker: stateChecker,
		data:         make(customData),
		header:       initSecretStateHeader(mmap),
		mmap:         mmap,
	}

	if s.header.version != 1 {
		return nil, fmt.Errorf("unsupported memfd-secret state version %d", s.header.version)
	}
	if s.header.size > s.capacity() {
		return nil, fmt.Errorf("invalid header size %d for capacity %d", s.header.size, s.capacity())
	}

	// load the existing state from the mmaped file.
	if s.header.size > 0 {
		if err := json.Unmarshal(mmap[secretStateHeaderSize:secretStateHeaderSize+s.header.size], &s.data); err != nil {
			return nil, fmt.Errorf("cannot unmarshal memfd-secret state: %w", err)
		}
	}

	// The finalizer runs on the GC goroutine without holding the state lock.
	// It only runs once the state is unreachable, so no other goroutine can
	// be accessing it and it can release the resources directly.
	runtime.SetFinalizer(s, (*secretState).closeLocked)
	return s, nil
}
