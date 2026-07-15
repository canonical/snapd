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
	"sync"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/systemd/fdstore"
	"golang.org/x/sys/unix"
)

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

var (
	secretStateOnce SecretState
	secretStateMu   = sync.RWMutex{}

	ErrInsufficientCapacity = errors.New("insufficient capacity in secret state")
)

func OpenSecretState() (retState SecretState, retErr error) {
	secretStateMu.Lock()
	defer secretStateMu.Unlock()

	if secretStateOnce != nil {
		return nil, fmt.Errorf("secret state already opened")
	}

	defer func() {
		if retErr != nil {
			logger.Noticef("cannot open memfd-secret backed secret state: %v", retErr)
			logger.Noticef("falling back to memory backed secret state instead")
			retState = &inMemorySecretState{
				data: make(customData),
			}
			runtime.SetFinalizer(retState, (*inMemorySecretState).Close)

			retErr = nil
			secretStateOnce = retState

			// best-effort removal from fdstore on error
			if err := fdstoreRemove(fdstore.FdNameMemfdSecretState); err != nil && !errors.Is(err, fdstore.ErrNotFound) {
				logger.Noticef("cannot remove memfd-secret state from fdstore: %v", err)
			}
			return
		}
	}()

	f, err := fdstoreGet(fdstore.FdNameMemfdSecretState)
	if errors.Is(err, fdstore.ErrNotFound) {
		f, err = createMemfdSecretFile(memfdSecretMinSize)
		if err != nil {
			return nil, fmt.Errorf("cannot create memfd-secret file: %w", err)
		}
		if err := fdstoreAdd(fdstore.FdNameMemfdSecretState, f); err != nil {
			return nil, fmt.Errorf("cannot add memfd-secret state: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("cannot get memfd-secret state: %w", err)
	}

	finfo, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("cannot stat memfd-secret state: %w", err)
	}

	if finfo.Size() < memfdSecretDataOffset {
		// XXX: file does not even fit the header, consider removing the file
		// and creating a new one with the correct size.
		return nil, fmt.Errorf("size %d is too small", finfo.Size())
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

	s := &memfdSecretState{
		f:      f,
		header: initMemfdSecretHeader(mmap),
		mmap:   mmap,
	}
	runtime.SetFinalizer(s, (*memfdSecretState).Close)

	if s.header.version != 1 {
		return nil, fmt.Errorf("unsupported memfd-secret state version %d", s.header.version)
	}
	if int(s.header.size) > s.capacity() {
		return nil, fmt.Errorf("invalid header size %d for capacity %d", s.header.size, s.capacity())
	}

	s.data = make(customData)
	if s.header.size > 0 {
		if err := json.Unmarshal(mmap[memfdSecretDataOffset:memfdSecretDataOffset+int(s.header.size)], &s.data); err != nil {
			return nil, fmt.Errorf("cannot unmarshal memfd-secret state: %w", err)
		}
	}

	secretStateOnce = s
	return s, nil
}

type inMemorySecretState struct {
	data customData

	closed bool
}

func (s *inMemorySecretState) Get(key string, value any) error {
	secretStateMu.RLock()
	defer secretStateMu.RUnlock()

	if s.closed {
		return fmt.Errorf("internal error: attempt to get key %q on closed state", key)
	}

	return s.data.get(key, value)
}

func (s *inMemorySecretState) Has(key string) bool {
	secretStateMu.RLock()
	defer secretStateMu.RUnlock()

	if s.closed {
		return false
	}

	return s.data.has(key)
}

func (s *inMemorySecretState) Set(key string, value any) error {
	secretStateMu.Lock()
	defer secretStateMu.Unlock()

	if s.closed {
		return fmt.Errorf("internal error: attempt to set key %q on closed state", key)
	}

	s.data.set(key, value)
	return nil
}

func (s *inMemorySecretState) Close() error {
	secretStateMu.Lock()
	defer secretStateMu.Unlock()

	if s == nil || s.closed {
		return nil
	}

	s.closed = true
	secretStateOnce = nil
	return nil
}

var (
	fdstoreAdd    = fdstore.Add
	fdstoreGet    = fdstore.Get
	fdstoreRemove = fdstore.Remove

	unixMmap   = unix.Mmap
	unixMunmap = unix.Munmap
)

const (
	memfdSecretMinSize = 1024 * 8 // 8KB
	memfdSecretMagic   = uint32(0x04081999)

	// The first 128 bytes of the file are reserved for the header, and the secret data starts at offset 128.
	memfdSecretDataOffset = 128
)

type memfdSecretState struct {
	data customData

	f      *os.File
	header memfdSecretHeader
	mmap   []byte

	closed bool
}

func (s *memfdSecretState) Get(key string, value any) error {
	secretStateMu.RLock()
	defer secretStateMu.RUnlock()

	if s.closed {
		return fmt.Errorf("internal error: attempt to get key %q on closed state", key)
	}

	return s.data.get(key, value)
}

func (s *memfdSecretState) Has(key string) bool {
	secretStateMu.RLock()
	defer secretStateMu.RUnlock()

	if s.closed {
		return false
	}

	return s.data.has(key)
}

func (s *memfdSecretState) Set(key string, value any) error {
	secretStateMu.Lock()
	defer secretStateMu.Unlock()

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

	needed := len(p)
	if s.capacity() < needed {
		// revert the change since the new state does not fit
		if hadPrev {
			s.data[key] = prevValue
		} else {
			delete(s.data, key)
		}
		return fmt.Errorf("cannot set key %q: %w", key, ErrInsufficientCapacity)
	}

	prevSize := int(s.header.size)
	copy(s.mmap[memfdSecretDataOffset:], p)
	// If the serialized blob shrank, wipe the now-unused tail so old secrets
	// don't remain readable in the backing memfd.
	if prevSize > needed {
		prev := s.mmap[memfdSecretDataOffset+needed : memfdSecretDataOffset+prevSize]
		// TODO:GOVERSION: use clear() once we are on go>=1.21
		for i := range prev {
			prev[i] = 0
		}
	}
	s.header.size = uint64(needed)
	s.header.writeTo(s.mmap[:memfdSecretDataOffset])
	return nil
}

func (s *memfdSecretState) capacity() int {
	return len(s.mmap) - memfdSecretDataOffset
}

func (s *memfdSecretState) Close() error {
	secretStateMu.Lock()
	defer secretStateMu.Unlock()

	return s.closeLocked()
}

func (s *memfdSecretState) closeLocked() error {
	if s == nil || s.closed {
		return nil
	}

	s.closed = true
	if s.mmap != nil {
		if err := unixMunmap(s.mmap); err != nil {
			return err
		}
		s.mmap = nil
	}
	if s.f != nil {
		if err := s.f.Close(); err != nil {
			return err
		}
		s.f = nil
	}

	secretStateOnce = nil
	return nil
}

type memfdSecretHeader struct {
	magic   uint32
	version uint16
	size    uint64
}

func initMemfdSecretHeader(data []byte) memfdSecretHeader {
	header := memfdSecretHeader{
		magic: binary.LittleEndian.Uint32(data[0:4]),
	}
	if header.magic != memfdSecretMagic {
		// unknown magic, wipe the whole file to avoid leaking
		// secrets from previous usage.
		// TODO:GOVERSION: use clear() once we are on go>=1.21
		for i := range data {
			data[i] = 0
		}

		// initialize header
		header = memfdSecretHeader{
			magic:   memfdSecretMagic,
			version: 1,
			size:    0,
		}
		header.writeTo(data[:memfdSecretDataOffset])
	} else {
		header.version = binary.LittleEndian.Uint16(data[4:6])
		header.size = binary.LittleEndian.Uint64(data[6:14])
	}
	return header
}

func (h *memfdSecretHeader) writeTo(data []byte) {
	binary.LittleEndian.PutUint32(data[0:4], h.magic)
	binary.LittleEndian.PutUint16(data[4:6], h.version)
	binary.LittleEndian.PutUint64(data[6:14], h.size)
}

// create a memfd-secret backed file
func createMemfdSecretFile(size uint64) (*os.File, error) {
	fd, err := unix.MemfdSecret(0)
	if err != nil {
		return nil, err
	}
	// TODO: Use raw fcntl and check for errors.
	unix.CloseOnExec(fd)

	f := os.NewFile(uintptr(fd), "memfd-secret")
	// memfd-secret files are created with size 0, so we need
	// to truncate it to the desired size.
	if err := f.Truncate(int64(size)); err != nil {
		return nil, err
	}
	return f, nil
}
