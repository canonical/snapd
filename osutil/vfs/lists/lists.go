// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

// Package lists implements a type-safe linked list where list nodes are
// embedded in larger types. A single type may contain any fixed number of list
// nodes, and thus participate in identical number of lists.
package lists

import "unsafe"

// Node is a pair of pointers to nodes of the same type.
//
// Node is embedded within another type T, so that a list of values of T may be
// created, without the necessity of separate allocation of Node structures.
//
// The zero value of a node is lazy-initialized to point to itself.
type Node[T any] struct {
	prev, next *Node[T]
}

// lazyInit initializes the node to point to itself.
func (n *Node[T]) lazyInit() {
	if n.prev == nil && n.next == nil {
		n.prev = n
		n.next = n
	}
}

// Unlink removes the node from the list it may be a member of.
func (n *Node[T]) Unlink() {
	n.lazyInit()

	next := n.next
	prev := n.prev

	prev.next = next
	next.prev = prev
	n.prev = n
	n.next = n
}

// Unlinked returns true if a node is not a member of any list.
//
// The zero value of a node is unlinked.
func (n *Node[T]) Unlinked() bool {
	return (n.next == nil && n.prev == nil) || (n.next == n && n.prev == n)
}

// linkAfter arranges pointers so that [other] is after [n].
func (n *Node[T]) linkAfter(other *Node[T]) {
	n.lazyInit()
	other.lazyInit()

	next := n.next

	other.prev = n
	other.next = next
	n.next = other
	next.prev = other
}

// linkBefore arranges points so that [other] is before [n].
func (n *Node[T]) linkBefore(other *Node[T]) {
	n.lazyInit()
	other.lazyInit()

	prev := n.prev

	other.prev = prev
	other.next = n
	n.prev = other
	prev.next = other
}

// List is a head of circular, doubly-linked list of elements of the same type T.
// The support type O provides an offset of Node[T] within T.
//
// The zero value has length zero and is empty.
type List[T any, O Offsetter[T]] struct {
	head Node[T]
}

// Empty returns true if the list has no elements.
func (l *List[T, O]) Empty() bool {
	return l.head.Unlinked()
}

// Len counts and returns the number of elements of the list.
func (l *List[T, O]) Len() int {
	var c int
	for n := l.head.next; n != nil && n != &l.head; n = n.next {
		c++
	}
	return c
}

// Append appends an element to the end of the list.
//
// The element e needs a Node[T] field for each list it is a member of.
func (l *List[T, O]) Append(e *T) {
	var o O
	l.head.linkBefore(nodePtr(e, o.Offset(nil)))
}

// Prepend prepends an element to the start of the list.
func (l *List[T, O]) Prepend(v *T) {
	var o O
	l.head.linkAfter(nodePtr(v, o.Offset(nil)))
}

// FirstToLast returns an iterator over elements of the list from first to last.
//
// It is safe to call [Node.Unlink] on the node that participates in the list.
// Iteration always advances through the original chain of nodes.
func (l *List[T, O]) FirstToLast() Seq[*T] {
	var o O
	off := o.Offset(nil)
	return func(yield func(*T) bool) {
		var next *Node[T]
		for n := l.head.next; n != nil && n != &l.head; n = next {
			next = n.next
			if !yield(containerPtr(n, off)) {
				return
			}
		}
	}
}

// LastToFirst returns an iterator over elements of the list from last to first.
//
// It is safe to call [Node.Unlink] on the node that participates in the list.
// Iteration always advances through the original chain of nodes.
func (l *List[T, O]) LastToFirst() Seq[*T] {
	var o O
	off := o.Offset(nil)
	return func(yield func(*T) bool) {
		var prev *Node[T]
		for n := l.head.prev; n != nil && n != &l.head; n = prev {
			prev = n.prev
			if !yield(containerPtr(n, off)) {
				return
			}
		}
	}
}

// Offsetter provides an offset of a particular [Node[T]] within [T].
type Offsetter[T any] interface {
	Offset(*T) uintptr
}

// nodePtr returns a pointer to node within a container.
func nodePtr[T any](c *T, off uintptr) *Node[T] {
	// This relies on valid use of unsafe.Pointer number (3) since we can
	// guarantee that returned pointer points to the same object.
	p := unsafe.Pointer(uintptr(unsafe.Pointer(c)) + off)
	return (*Node[T])(p)
}

// containerPtr returns a pointer to a container of a given node.
func containerPtr[T any](n *Node[T], off uintptr) *T {
	// This relies on valid use of unsafe.Pointer number (3) since we can
	// guarantee that returned pointer points to the same object.
	p := unsafe.Pointer(uintptr(unsafe.Pointer(n)) - off)
	return (*T)(p)
}
