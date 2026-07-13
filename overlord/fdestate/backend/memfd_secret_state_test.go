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

package backend_test

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/systemd/fdstore"
	"github.com/snapcore/snapd/testutil"
)

type secretStateSuite struct {
	testutil.BaseTest

	fdstoreFile *os.File
	ops         []string
	failOn      map[string]bool
}

var _ = Suite(&secretStateSuite{})

func dupFile(name fdstore.FdName, f *os.File) (*os.File, error) {
	duplicatedFd, err := unix.Dup(int(f.Fd()))
	if err != nil {
		return nil, err
	}
	unix.CloseOnExec(duplicatedFd)

	return os.NewFile(uintptr(duplicatedFd), string(name)), nil
}

func (s *secretStateSuite) fdstoreGet(name fdstore.FdName) (*os.File, error) {
	s.ops = append(s.ops, fmt.Sprintf("fdstore-get: %s", name))

	if s.failOn["fdstore-get"] {
		return nil, fmt.Errorf("boom")
	}

	if name == fdstore.FdNameMemfdSecretState && s.fdstoreFile != nil {
		// duplicate fd
		return dupFile(name, s.fdstoreFile)
	}
	return nil, fdstore.ErrNotFound
}

func (s *secretStateSuite) fdstoreAdd(name fdstore.FdName, f *os.File) error {
	s.ops = append(s.ops, fmt.Sprintf("fdstore-add: %s", name))

	if s.failOn["fdstore-add"] {
		return fmt.Errorf("boom")
	}

	if name != fdstore.FdNameMemfdSecretState {
		return fmt.Errorf("unexpected fdstore name: %s", name)
	}

	if s.fdstoreFile != nil {
		return fmt.Errorf("fdstore already has a file for %s", name)
	}
	duplicatedFile, err := dupFile(name, f)
	if err != nil {
		return err
	}
	s.fdstoreFile = duplicatedFile
	return nil
}

func (s *secretStateSuite) fdstoreRemove(name fdstore.FdName) error {
	s.ops = append(s.ops, fmt.Sprintf("fdstore-remove: %s", name))

	if s.failOn["fdstore-remove"] {
		return fmt.Errorf("boom")
	}

	if name != fdstore.FdNameMemfdSecretState {
		return fmt.Errorf("unexpected fdstore name: %s", name)
	}
	if s.fdstoreFile == nil {
		return fdstore.ErrNotFound
	}
	s.fdstoreFile.Close()
	s.fdstoreFile = nil
	return nil
}

func (s *secretStateSuite) mmap(fd int, offset int64, length int, prot int, flags int) ([]byte, error) {
	s.ops = append(s.ops, fmt.Sprintf("mmap: %d", length))

	if s.failOn["mmap"] {
		return nil, fmt.Errorf("boom")
	}

	return unix.Mmap(fd, offset, length, prot, flags)
}

func (s *secretStateSuite) munmap(b []byte) error {
	s.ops = append(s.ops, fmt.Sprintf("munmap: %d", len(b)))

	if s.failOn["munmap"] {
		return fmt.Errorf("boom")
	}

	return unix.Munmap(b)
}

func (s *secretStateSuite) SetUpTest(c *C) {
	backend.ResetSecretState()

	s.fdstoreFile = nil
	s.ops = []string{}
	s.failOn = make(map[string]bool)

	s.AddCleanup(backend.MockFdstoreGet(s.fdstoreGet))
	s.AddCleanup(backend.MockFdstoreAdd(s.fdstoreAdd))
	s.AddCleanup(backend.MockFdstoreRemove(s.fdstoreRemove))
	s.AddCleanup(backend.MockUnixMmap(s.mmap))
	s.AddCleanup(backend.MockUnixMunmap(s.munmap))
}

func (s *secretStateSuite) TearDownTest(c *C) {
	if s.fdstoreFile != nil {
		s.fdstoreFile.Close()
		s.fdstoreFile = nil
	}
}

func (s *secretStateSuite) testMemfdSecretStateHappy(c *C, stateBackend string) {

	switch stateBackend {
	case "memfd-secret":
		// default behavior, OpenSecretState() will use the memfd-secret backend.
	case "in-memory":
		// force OpenSecretState() to use the in-memory fallback path.
		s.failOn["fdstore-get"] = true
	default:
		c.Fatalf("unsupported state backend: %s", stateBackend)
	}

	logbuf, restore := logger.MockLogger()
	defer restore()

	expectedOps := []string{}

	// Open and initialize the secret state
	secretState, err := backend.OpenSecretState()
	c.Assert(err, IsNil)
	c.Assert(secretState, NotNil)

	if stateBackend == "in-memory" {
		c.Assert(logbuf.String(), testutil.Contains, "cannot open memfd-secret backed secret state: cannot get memfd-secret state: boom")
		c.Assert(logbuf.String(), testutil.Contains, "falling back to memory backed secret state instead")
	}

	if stateBackend == "memfd-secret" {
		expectedOps = append(expectedOps,
			// first try to get the memfd-secret-state from fdstore fails, and
			// since it doesn't exist, it should create a new one, add it to
			// fdstore and mmap it.
			"fdstore-get: memfd-secret-state",
			"fdstore-add: memfd-secret-state",
			"mmap: 8192",
		)
	} else {
		// fallback cleanup will attempt to remove the memfd-secret-state from fdstore
		expectedOps = append(expectedOps,
			"fdstore-get: memfd-secret-state", // fails and triggers fallback to in-memory
			"fdstore-remove: memfd-secret-state",
		)
	}
	c.Assert(s.ops, DeepEquals, expectedOps)

	// Secret state can only be opened once, so trying to open it again should fail
	_, err = backend.OpenSecretState()
	c.Check(err, ErrorMatches, "secret state already opened")

	// Get a non-existing key
	var value string
	err = secretState.Get("non-existing", &value)
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// Set a key
	secretState.Set("key-1", "some-value")

	// Get the key
	var val string
	err = secretState.Get("key-1", &val)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "some-value")

	// Check if the key exists
	exists := secretState.Has("key-1")
	c.Assert(exists, Equals, true)

	// Remove the key by setting it to nil
	secretState.Set("key-1", nil)

	// Check if the key exists after removal
	exists = secretState.Has("key-1")
	c.Assert(exists, Equals, false)

	// Set another key
	secretState.Set("key-2", "another-value")

	// Close the secret state
	err = backend.CloseSecretState(secretState)
	c.Assert(err, IsNil)

	// All operations should fail after closing the secret state
	c.Check(func() { secretState.Set("key-3", "another-value") }, PanicMatches, `internal error: attempt to set key "key-3" on closed state`)
	err = secretState.Get("key-3", &val)
	c.Check(err, ErrorMatches, `internal error: attempt to get key "key-3" on closed state`)
	exists = secretState.Has("key-3")
	c.Check(exists, Equals, false)

	// Reopen the secret state and check that the previous key is still there
	secretState, err = backend.OpenSecretState()
	c.Assert(err, IsNil)
	c.Assert(secretState, NotNil)

	if stateBackend == "memfd-secret" {
		expectedOps = append(expectedOps,
			// closing the previous state unmaps its mapping, then reopening
			// gets the existing memfd-secret-state from fdstore and mmaps it.
			"munmap: 8192",
			"fdstore-get: memfd-secret-state",
			"mmap: 8192",
		)
	} else {
		// again, fallback cleanup will attempt to remove the memfd-secret-state from fdstore
		expectedOps = append(expectedOps,
			"fdstore-get: memfd-secret-state", // fails and triggers fallback to in-memory
			"fdstore-remove: memfd-secret-state",
		)
	}
	c.Assert(s.ops, DeepEquals, expectedOps)

	// Check behavior after reopening.
	val = ""
	err = secretState.Get("key-2", &val)
	if stateBackend == "memfd-secret" {
		c.Check(err, IsNil)
		c.Assert(val, Equals, "another-value")
	} else {
		c.Check(err, testutil.ErrorIs, state.ErrNoState)
	}

	// no more operations should have been done
	c.Assert(s.ops, DeepEquals, expectedOps)
}

func (s *secretStateSuite) TestMemfdSecretStateHappyMemfdSecret(c *C) {
	const backend = "memfd-secret"
	s.testMemfdSecretStateHappy(c, backend)
}

func (s *secretStateSuite) TestMemfdSecretStateHappyInMemoryFallback(c *C) {
	const backend = "in-memory"
	s.testMemfdSecretStateHappy(c, backend)
}

func (s *secretStateSuite) TestMemfdSecretStateGrows(c *C) {
	expectedOps := []string{}

	// Open and initialize the secret state
	secretState, err := backend.OpenSecretState()
	c.Assert(err, IsNil)
	c.Assert(secretState, NotNil)

	expectedOps = append(expectedOps,
		// first try to get the memfd-secret-state from fdstore fails, and
		// since it doesn't exist, it should create a new one, add it to
		// fdstore and mmap it.
		"fdstore-get: memfd-secret-state",
		"fdstore-add: memfd-secret-state",
		"mmap: 8192",
	)
	c.Assert(s.ops, DeepEquals, expectedOps)

	memfdSecretState := secretState.(*backend.MemfdSecretState)
	// initial capacity is (8KB - 128B) for the header
	c.Assert(memfdSecretState.Capacity(), Equals, 1024*8-128)

	// Set a key with a large value to trigger growth
	largeValue := make([]byte, 1024*1024) // 1MB
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}
	secretState.Set("large-key", largeValue)

	// The capacity should have grown to accommodate the large value. The new
	// capacity should be at least (~1MB + 128B) for the header, and since the growth
	// doubles the size, it should be (2MB - 128B).
	c.Assert(memfdSecretState.Capacity(), Equals, 1024*1024*2-128)

	expectedOps = append(expectedOps,
		// growth mmaps the new (larger) file, unmaps the old mapping,
		// then swaps the file stored in fdstore.
		"mmap: 2097152",
		"munmap: 8192",
		"fdstore-remove: memfd-secret-state",
		"fdstore-add: memfd-secret-state",
	)
	c.Assert(s.ops, DeepEquals, expectedOps)

	// Get the key and verify the value
	var retrievedValue []byte
	err = secretState.Get("large-key", &retrievedValue)
	c.Assert(err, IsNil)
	c.Assert(retrievedValue, DeepEquals, largeValue)

	// Close the secret state
	err = backend.CloseSecretState(secretState)
	c.Assert(err, IsNil)

	expectedOps = append(expectedOps,
		// closing unmaps the current mapping
		"munmap: 2097152",
	)
	c.Assert(s.ops, DeepEquals, expectedOps)
}

func (s *secretStateSuite) TestMemfdSecretStateGrowFails(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	// Open and initialize the secret state
	secretState, err := backend.OpenSecretState()
	c.Assert(err, IsNil)
	c.Assert(secretState, NotNil)

	expectedOps := []string{
		"fdstore-get: memfd-secret-state",
		"fdstore-add: memfd-secret-state",
		"mmap: 8192",
	}
	c.Assert(s.ops, DeepEquals, expectedOps)

	// Make the fdstore add that happens during growth fail.
	s.failOn["fdstore-add"] = true

	// Setting a key with a large value triggers growth, which fails at the
	// fdstore add step. The failure must be cleaned up properly:
	//   1. close the old state which unmaps its mapping
	//   2. unmap the new mapping and remove the state from fdstore
	// The parent Set must panic.
	largeValue := make([]byte, 1024*1024) // 1MB
	c.Check(func() { secretState.Set("large-key", largeValue) }, PanicMatches,
		`internal error: cannot grow state data: boom`)

	expectedOps = append(expectedOps,
		// growth mmaps the new (larger) file
		"mmap: 2097152",
		// growth closes the old state, which unmaps the old mapping
		"munmap: 8192",
		// growth removes the old file from fdstore
		"fdstore-remove: memfd-secret-state",
		// growth tries to add the new file to fdstore, which fails here
		"fdstore-add: memfd-secret-state",
		// cleanup unmaps the new mapping
		"munmap: 2097152",
		// cleanup does a best-effort removal from fdstore
		"fdstore-remove: memfd-secret-state",
	)
	c.Assert(s.ops, DeepEquals, expectedOps)

	c.Assert(logbuf.String(), testutil.Contains, "internal error: cannot grow state data: boom")

	// The failed growth fully closed the previous state and reset the global
	// secret state, so a fresh secret state can be opened again.
	s.failOn["fdstore-add"] = false
	secretState, err = backend.OpenSecretState()
	c.Assert(err, IsNil)
	c.Assert(secretState, NotNil)
	c.Assert(secretState.Has("large-key"), Equals, false)
	c.Assert(backend.CloseSecretState(secretState), IsNil)
}
