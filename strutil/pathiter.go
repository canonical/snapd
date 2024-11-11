// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package strutil

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PathIterator traverses through parts (directories and files) of some
// path. The filesystem is never consulted, traversal is done purely in memory.
//
// The iterator is useful in implementing secure traversal of absolute paths
// using the common idiom of opening the root directory followed by a chain of
// openat calls.
//
// A simple example on how to use the iterator:
// ```
// iter := NewPathIterator(path)
//
//	for iter.Next() {
//	   // Use iter.CurrentBase() with openat(2) family of functions.
//	   // Use iter.CurrentPath() or iter.CurrentDir() for context.
//	}
//
// ```
type PathIterator struct {
	// Contains full clean path (the only exception to cleaniness is a possible trailing slash)
	path string
	// Left and right are indices of the start and end of the element indicated in CurrentBase
	left, right int
	// The depth-th path element pointed to by the iterator
	depth int
}

// NewPathIterator returns an iterator for traversing the given path.
// The path must be clean (except for a possible trailing slash).
func NewPathIterator(path string) (*PathIterator, error) {
	cleanPath := filepath.Clean(path)
	if cleanPath != path && cleanPath+"/" != path {
		return nil, fmt.Errorf("cannot iterate over unclean path %q", path)
	}
	return &PathIterator{path: path}, nil
}

// Path returns the entire path being traversed.
func (iter *PathIterator) Path() string {
	return iter.path
}

// CurrentBase returns the last element of the current path.
// It removes any trailing slash unless the last element of the
// current path is the filesystem root.
// Before any iteration with Next, CurrentBase returns an empty string.
// Example:
//
//	iter := NewPathIterator("/foo/bar")  // iter.CurrentBase() == ""
//	iter.Next() // iter.CurrentBase() == "/" && iter.CurrentPath() == "/"
//	iter.Next() // iter.CurrentBase() == "foo" && iter.CurrentPath() == "/foo"
//	iter.Next() // iter.CurrentBase() == "bar" && iter.CurrentPath() == "/foo/bar"
func (iter *PathIterator) CurrentBase() string {
	// Only remove trailing slash if the iterator is not pointing to the filesystem root
	if iter.right > 1 && iter.path[iter.right-1:iter.right] == "/" {
		return iter.path[iter.left : iter.right-1]
	}
	return iter.path[iter.left:iter.right]
}

// CurrentPath returns the path up to the current depth.
// Before any iteration with Next, CurrentPath returns an empty string.
// Example:
//
//		iter := NewPathIterator("/foo/bar")  // iter.CurrentPath() == ""
//	 	iter.Next() // iter.CurrentPath() == "/" && iter.Depth() == 1
//	 	iter.Next() // iter.CurrentPath() == "/foo" && iter.Depth() == 2
//	 	iter.Next() // iter.CurrentPath() == "/foo/bar" && iter.Depth() == 3
func (iter *PathIterator) CurrentPath() string {
	// Only remove trailing slash if the iterator is not pointing to the filesystem root
	if iter.right > 1 && iter.path[iter.right-1:iter.right] == "/" {
		return iter.path[:iter.right-1]
	}
	return iter.path[:iter.right]
}

// Adds a trailing slash to the current path even if the current path does not have one.
// Before any iteration with Next, CurrentPathPlusSlash returns an empty string.
// Example:
//
//	    iter := NewPathIterator("/foo/bar")  // iter.CurrentPathPlusSlash() == ""
//		iter.Next() // iter.CurrentPathPlusSlash() == "/"
//		iter.Next() // iter.CurrentPathPlusSlash() == "/foo/"
//		iter.Next() // iter.CurrentPathPlusSlash() == "/foo/bar/"
func (iter *PathIterator) CurrentPathPlusSlash() string {
	// Only add an trailing slash if one isn't already present
	if iter.right > 0 && iter.path[iter.right-1:iter.right] != "/" {
		return iter.path[:iter.right] + "/"
	}
	return iter.path[:iter.right]
}

// CurrentDir returns all but the last element of the current path.
// The result always removes a trailing slash unless CurrentDir is the filesystem root.
// If the current path is empty because either Next has not yet been called or the
// current path only contains one element, then CurrentDir will return an empty string.
// Example:
//
//	iter := NewPathIterator("/foo/bar")  // iter.CurrentDir() == ""
//	iter.Next() // iter.CurrentDir() == "" && iter.CurrentPath() == "/"
//	iter.Next() // iter.CurrentDir() == "/" && iter.CurrentPath() == "/foo"
//	iter.Next() // iter.CurrentDir() == "/foo" && iter.CurrentPath() == "/foo/bar"
//
// Example:
//
//	iter := NewPathIterator("foo/bar")  // iter.CurrentDir() == ""
//	iter.Next() // iter.CurrentDir() == "" && iter.CurrentPath() == "foo"
//	iter.Next() // iter.CurrentDir() == "foo" && iter.CurrentPath() == "foo/bar"
func (iter *PathIterator) CurrentDir() string {
	if iter.left > 0 && iter.path[iter.left-1] == '/' && iter.path[:iter.left] != "/" {
		return iter.path[:iter.left-1]
	}
	return iter.path[:iter.left]
}

// IsCurrentBaseLeaf returns true if CurrentBase is the leaf of the path.
// Example:
//
//	iter := NewPathIterator("foo/bar")  // iter.IsCurrentBaseLeaf() == false
//	iter.Next() // iter.CurrentBase() == "foo" && iter.IsCurrentBaseLeaf() == false
//	iter.Next() // iter.CurrentBase() == "bar" && iter.IsCurrentBaseLeaf() == true
func (iter *PathIterator) IsCurrentBaseLeaf() bool {
	return iter.right == len(iter.path)
}

// Depth returns the directory depth of the current path.
//
// This is equal to the number of traversed directories, including that of the
// root directory.
func (iter *PathIterator) Depth() int {
	return iter.depth
}

// Next advances the iterator to the next name, returning true if one is found.
//
// If this method returns false then no change is made and all helper methods
// retain their previous return values.
func (iter *PathIterator) Next() bool {
	// Initial state
	// P: "foo/bar"
	// L:  ^
	// R:  ^
	//
	// Next is called
	// P: "foo/bar"
	// L:  ^  |
	// R:     ^
	//
	// Next is called
	// P: "foo/bar"
	// L:     ^   |
	// R:         ^

	// Next is called but returns false
	// P: "foo/bar"
	// L:     ^   |
	// R:         ^
	if iter.right >= len(iter.path) {
		return false
	}
	iter.left = iter.right
	if idx := strings.IndexRune(iter.path[iter.right:], '/'); idx != -1 {
		iter.right += idx + 1
	} else {
		iter.right = len(iter.path)
	}
	iter.depth++
	return true
}

// Rewind returns the iterator to the initial state, allowing the path to be traversed again.
func (iter *PathIterator) Rewind() {
	iter.left = 0
	iter.right = 0
	iter.depth = 0
}
