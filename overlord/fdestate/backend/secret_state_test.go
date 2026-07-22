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
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/systemd/fdstore"
	"github.com/snapcore/snapd/testutil"
)

type secretStateSuite struct {
	testutil.BaseTest

	fdstoreFile *os.File
	ops         []string
	failOn      map[string]error
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

	if s.failOn["fdstore-get"] != nil {
		return nil, s.failOn["fdstore-get"]
	}

	if name == fdstore.FdNameMemfdSecretState && s.fdstoreFile != nil {
		// duplicate fd
		return dupFile(name, s.fdstoreFile)
	}
	return nil, fdstore.ErrNotFound
}

func (s *secretStateSuite) fdstoreAdd(name fdstore.FdName, f *os.File) error {
	s.ops = append(s.ops, fmt.Sprintf("fdstore-add: %s", name))

	if s.failOn["fdstore-add"] != nil {
		return s.failOn["fdstore-add"]
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

	if s.failOn["fdstore-remove"] != nil {
		return s.failOn["fdstore-remove"]
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

	if s.failOn["mmap"] != nil {
		return nil, s.failOn["mmap"]
	}

	return unix.Mmap(fd, offset, length, prot, flags)
}

func (s *secretStateSuite) munmap(b []byte) error {
	s.ops = append(s.ops, fmt.Sprintf("munmap: %d", len(b)))

	if s.failOn["munmap"] != nil {
		return s.failOn["munmap"]
	}

	return unix.Munmap(b)
}

func (s *secretStateSuite) memfdSecret(flags int) (int, error) {
	s.ops = append(s.ops, "memfd-secret")

	if s.failOn["memfd-secret"] != nil {
		return 0, s.failOn["memfd-secret"]
	}

	return unix.MemfdSecret(flags)
}

func (s *secretStateSuite) memfdCreate(name string, flags int) (int, error) {
	s.ops = append(s.ops, fmt.Sprintf("memfd-create: %s", name))

	if s.failOn["memfd-create"] != nil {
		return 0, s.failOn["memfd-create"]
	}

	return unix.MemfdCreate(name, flags)
}

func (s *secretStateSuite) SetUpTest(c *C) {
	s.fdstoreFile = nil
	s.ops = []string{}
	s.failOn = make(map[string]error)

	s.AddCleanup(backend.MockFdstoreGet(s.fdstoreGet))
	s.AddCleanup(backend.MockFdstoreAdd(s.fdstoreAdd))
	s.AddCleanup(backend.MockFdstoreRemove(s.fdstoreRemove))
	s.AddCleanup(backend.MockUnixMmap(s.mmap))
	s.AddCleanup(backend.MockUnixMunmap(s.munmap))
	s.AddCleanup(backend.MockUnixMemfdSecret(s.memfdSecret))
	s.AddCleanup(backend.MockUnixMemfdCreate(s.memfdCreate))
}

func (s *secretStateSuite) TearDownTest(c *C) {
	if s.fdstoreFile != nil {
		s.fdstoreFile.Close()
		s.fdstoreFile = nil
	}
}

func (s *secretStateSuite) testMemfdSecretStateHappy(c *C, stateBackend string) {
	logbuf, restore := logger.MockDebugLogger()
	defer restore()

	switch stateBackend {
	case "memfd-secret":
		// default behavior, OpenSecretState() will use the memfd-secret backend.

	case "memfd-create":
		// force OpenSecretStateFile() to use the memfd-create fallback path.
		s.failOn["memfd-secret"] = unix.ENOSYS
	default:
		c.Fatalf("unsupported state backend: %s", stateBackend)
	}

	expectedOps := []string{}

	expectedOps = append(expectedOps,
		"fdstore-get: memfd-secret-state", // get the secret state file, doesn't exist
		"memfd-secret",                    // create a new memfd-secret file
	)
	if stateBackend == "memfd-create" {
		expectedOps = append(expectedOps, "memfd-create: secret-state") // fallback to memfd-create
	}
	expectedOps = append(expectedOps, "fdstore-add: memfd-secret-state") // add the new secret state file to fdstore

	// Open and initialize the secret state
	secretState, err := backend.OpenSecretState()
	c.Assert(err, IsNil)
	c.Assert(secretState, NotNil)

	if stateBackend == "memfd-secret" && strutil.ListContains(s.ops, "memfd-create: secret-state") {
		// On kernels without memfd_secret support, OpenSecretStateFile will
		// fallback to the memfd-create implementation.
		c.Skip("memfd-secret is not supported")
	}

	expectedOps = append(expectedOps, "mmap: 8192")
	c.Assert(s.ops, DeepEquals, expectedOps)

	if stateBackend == "memfd-create" {
		c.Assert(logbuf.String(), testutil.Contains, "cannot create memfd-secret (function not implemented), falling back to memfd-create")
	}

	// Get a non-existing key
	var value string
	err = secretState.Get("non-existing", &value)
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	// Set a key
	err = secretState.Set("key-1", "some-value")
	c.Assert(err, IsNil)

	// Get the key
	var val string
	err = secretState.Get("key-1", &val)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "some-value")

	// Check if the key exists
	exists := secretState.Has("key-1")
	c.Assert(exists, Equals, true)

	// Remove the key by setting it to nil
	err = secretState.Set("key-1", nil)
	c.Assert(err, IsNil)

	// Check if the key exists after removal
	exists = secretState.Has("key-1")
	c.Assert(exists, Equals, false)

	// Set another key
	err = secretState.Set("key-2", "another-value")
	c.Assert(err, IsNil)

	// Close the secret state
	err = secretState.Close()
	c.Assert(err, IsNil)

	expectedOps = append(expectedOps,
		// closing the previous state unmaps its mapping, then reopening
		// gets the existing memfd-secret-state from fdstore and mmaps it.
		"munmap: 8192",
	)
	c.Assert(s.ops, DeepEquals, expectedOps)

	// All operations should fail after closing the secret state
	err = secretState.Set("key-3", "another-value")
	c.Check(err, ErrorMatches, `internal error: attempt to set key "key-3" on closed state`)
	err = secretState.Get("key-3", &val)
	c.Check(err, ErrorMatches, `internal error: attempt to get key "key-3" from closed state`)
	exists = secretState.Has("key-3")
	c.Check(exists, Equals, false)

	// Reopen the secret state and check that the previous key is still there
	secretState, err = backend.OpenSecretState()
	c.Assert(err, IsNil)
	c.Assert(secretState, NotNil)

	expectedOps = append(expectedOps,
		"fdstore-get: memfd-secret-state",
		"mmap: 8192",
	)
	c.Assert(s.ops, DeepEquals, expectedOps)

	// Check behavior after reopening.
	val = ""
	err = secretState.Get("key-2", &val)
	c.Check(err, IsNil)
	c.Assert(val, Equals, "another-value")

	err = secretState.Close()
	c.Assert(err, IsNil)
	expectedOps = append(expectedOps,
		"munmap: 8192",
	)

	// no more operations should have been done
	c.Assert(s.ops, DeepEquals, expectedOps)
}

func (s *secretStateSuite) TestMemfdSecretStateHappyMemfdSecret(c *C) {
	const stateBackend = "memfd-secret"
	s.testMemfdSecretStateHappy(c, stateBackend)
}

func (s *secretStateSuite) TestMemfdSecretStateHappyMemfdCreate(c *C) {
	const stateBackend = "memfd-create"
	s.testMemfdSecretStateHappy(c, stateBackend)
}

func (s *secretStateSuite) testMemfdSecretStateSetTooLarge(c *C, stateBackend string) {
	logbuf, restore := logger.MockDebugLogger()
	defer restore()

	switch stateBackend {
	case "memfd-secret":
		// default behavior, OpenSecretState() will use the memfd-secret backend.

	case "memfd-create":
		// force OpenSecretStateFile() to use the memfd-create fallback path.
		s.failOn["memfd-secret"] = errors.New("boom!")
	default:
		c.Fatalf("unsupported state backend: %s", stateBackend)
	}

	expectedOps := []string{}

	expectedOps = append(expectedOps,
		"fdstore-get: memfd-secret-state", // get the secret state file, doesn't exist
		"memfd-secret",                    // create a new memfd-secret file
	)
	if stateBackend == "memfd-create" {
		expectedOps = append(expectedOps, "memfd-create: secret-state") // fallback to memfd-create
	}
	expectedOps = append(expectedOps, "fdstore-add: memfd-secret-state") // add the new secret state file to fdstore

	secretState, err := backend.OpenSecretState()
	c.Assert(err, IsNil)
	c.Assert(secretState, NotNil)

	if stateBackend == "memfd-secret" && strutil.ListContains(s.ops, "memfd-create: secret-state") {
		// On kernels without memfd_secret support, OpenSecretStateFile will
		// fallback to the memfd-create implementation.
		c.Skip("memfd-secret is not supported by the kernel")
	}

	expectedOps = append(expectedOps, "mmap: 8192")
	c.Assert(s.ops, DeepEquals, expectedOps)

	if stateBackend == "memfd-create" {
		c.Assert(logbuf.String(), testutil.Contains, "cannot create memfd-secret (boom!), falling back to memfd-create")
	}

	memfdSecretState := secretState.(*backend.MemfdSecretState)
	// capacity is fixed at (8KB - 32B) for the header
	c.Assert(memfdSecretState.Capacity(), Equals, uint64(1024*8-32))

	// Setting a key with a value that does not fit in the fixed-size backing
	// store fails.
	largeValue := make([]byte, 1024*1024) // 1MB
	for i := range largeValue {
		largeValue[i] = byte(i % 256)
	}
	err = secretState.Set("large-key", largeValue)
	c.Assert(err, ErrorMatches, `cannot set key "large-key": insufficient capacity in secret state`)
	c.Assert(err, testutil.ErrorIs, backend.ErrInsufficientCapacity)

	// The failed Set left no trace: the key was not stored and the capacity
	// is unchanged.
	c.Assert(secretState.Has("large-key"), Equals, false)
	c.Assert(memfdSecretState.Capacity(), Equals, uint64(1024*8-32))

	// No growth-related operations should have been performed.
	c.Assert(s.ops, DeepEquals, expectedOps)

	// A value that fits can still be set afterwards.
	c.Assert(secretState.Set("small-key", "value"), IsNil)
	var val string
	c.Assert(secretState.Get("small-key", &val), IsNil)
	c.Assert(val, Equals, "value")

	// Close the secret state
	err = secretState.Close()
	c.Assert(err, IsNil)

	expectedOps = append(expectedOps,
		// closing unmaps the current mapping
		"munmap: 8192",
	)
	c.Assert(s.ops, DeepEquals, expectedOps)
}

func (s *secretStateSuite) TestMemfdSecretStateSetTooLargeMemfdSecret(c *C) {
	const stateBackend = "memfd-secret"
	s.testMemfdSecretStateSetTooLarge(c, stateBackend)
}

func (s *secretStateSuite) TestMemfdSecretStateSetTooLargeMemfdCreate(c *C) {
	const stateBackend = "memfd-create"
	s.testMemfdSecretStateSetTooLarge(c, stateBackend)
}
