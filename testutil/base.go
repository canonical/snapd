// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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

package testutil

import (
	"os"
	"reflect"

	"gopkg.in/check.v1"
)

// BaseTest is a structure used as a base test suite for all the snappy
// tests.
type BaseTest struct {
	cleanupHandlers []func()
}

// SetUpTest prepares the cleanup.
func (s *BaseTest) SetUpTest(c *check.C) {
	if len(s.cleanupHandlers) != 0 {
		panic("BaseTest cleanup handlers were not consumed before a new test start, missing BaseTest.TearDownTest call?")
	}

	// When unit tests are called with SNAPD_DEBUG=1, we tend to get some failures due to the
	// mismatch in the expected output and actual output. Instead of doing an unset SNAPD_DEBUG
	// in all those cases, adding it in here - a common test helper function to be called by inidividual unit tests.
	os.Unsetenv("SNAPD_DEBUG")
}

// TearDownTest cleans up the channel.ini files in case they were changed by
// the test.
// It also runs the cleanup handlers
func (s *BaseTest) TearDownTest(c *check.C) {
	// run cleanup handlers in reverse order and clear the slice
	n := len(s.cleanupHandlers)
	for i := range s.cleanupHandlers {
		f := s.cleanupHandlers[n-1-i]
		f()
	}
	s.cleanupHandlers = nil
}

// AddCleanup adds a new cleanup function to the test.
func (s *BaseTest) AddCleanup(f func()) {
	s.cleanupHandlers = append(s.cleanupHandlers, f)
}

// Backup a single element before further mocking.
func Backup[T any](mockable *T) (restore func()) {
	backup := *mockable

	return func() {
		*mockable = backup
	}
}

// Backup the specified list of elements before further mocking.
func BackupMany(mockablesByPtr ...interface{}) (restore func()) {
	// TODO find a type safe way of doing this when individual elements are
	// of different type
	backup := backupMockables(mockablesByPtr)

	return func() {
		for i, ptr := range mockablesByPtr {
			mockedPtr := reflect.ValueOf(ptr)
			mockedPtr.Elem().Set(backup[i].Elem())
		}
	}
}

func backupMockables(mockablesByPtr []interface{}) (backup []*reflect.Value) {
	backup = make([]*reflect.Value, len(mockablesByPtr))

	for i, ptr := range mockablesByPtr {
		mockedPtr := reflect.ValueOf(ptr)

		if mockedPtr.Type().Kind() != reflect.Ptr {
			panic("Backup: each mockable must be passed by pointer!")
		}

		saved := reflect.New(mockedPtr.Elem().Type())
		saved.Elem().Set(mockedPtr.Elem())
		backup[i] = &saved
	}
	return backup
}

// Mock provides a type safe way of mocking a single thing.
func Mock[T any](mockable *T, newVal T) (restore func()) {
	restore = Backup(mockable)
	*mockable = newVal
	return restore
}
