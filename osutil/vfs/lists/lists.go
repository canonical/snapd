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
// embedded in larger structures. A single structure may contain a fixed number
// of list nodes allowing it to participate in the same number of lists.
//
// Two list types are provided, [List] and [HeadlessList]. They differ in the
// use of a list head node. A list head is a special node that does not
// correspond to an element of the list, but serves as the anchor, and a way to
// begin iteration, either forward or backward.
//
// A [List] may be used to track any number of elements of a type T if said
// type T embeds a [Node[T]]. Note that a [List] may also be a member field of
// T, but a dedicated Node[T] is always required.
//
// In contrast [HeadlessList] can only be used to track elements of the same
// type that stores it as a member field.
//
// Both [List] and [HeadlessList] require a participating [Offsetter] type to
// provide the offset of the [Node[T]] within the containing structure.
package lists

// Node is a pair of pointers to nodes of the same type.
//
// Node is embedded within another type T, so that a list of values of T may be
// created, without the necessity of separate allocation of Node structures.
//
// The zero value of a node is lazy-initialized to point to itself.
//
// Each node stores a pointer to the container it is a part of. Node functions
// other than [InitializeNode] do not set this field. The container is used by
// higher-level constructs that use it as a way to return the container element
// when iterating over nodes.
type Node[T any] struct {
	prev, next *Node[T]
	container  *T
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

// linkBefore arranges pointers so that [other] is before [n].
func (n *Node[T]) linkBefore(other *Node[T]) {
	n.lazyInit()
	other.lazyInit()

	prev := n.prev

	other.prev = prev
	other.next = n
	n.prev = other
	prev.next = other
}

// NodePointerer is an interface that types must implement to provide access
// to their embedded Node[T].
//
// This is used by [containedNode] to create a NodeContainer[T] from a T.
type NodePointerer[T any] interface {
	NodePointer(*T) *Node[T]
}

// containedNode is a private type that wraps a pointer to a node embedded in a
// structure of type T. It is used to provide type safety for list operations,
// by ensuring that the node container pointer was set correctly.
type containedNode[T any] struct {
	n *Node[T]
}

// ContainedNode returns a private type that ensures node container pointer is
// initialized.
//
// The returned value is suitable for use with List.Append and List.Prepend.
func ContainedNode[NP NodePointerer[T], T any](c *T) containedNode[T] {
	return containedNode[T]{n: InitializeNode[NP, T](c)}
}

// InitializeNode stores the pointer [c] inside the node container field.
func InitializeNode[NP NodePointerer[T], T any](c *T) *Node[T] {
	if c == nil {
		panic("cannot initialize Node: container is nil")
	}

	var helper NP
	n := helper.NodePointer(c)
	if n == nil {
		panic("cannot initialize Node: node pointer is nil (computed by NodePointer)")
	}

	n.lazyInit()
	n.container = c
	return n
}

// List is a head of circular, doubly-linked list of elements of the same type
// T.  The type T must have a related type that implements the NodePointerer[T]
// interface.
//
// The zero value has length zero and is empty.
type List[T any] struct {
	head Node[T]
}

// InitializeList ensures that the head node of the list links to itself.
//
// Note that unlike [InitializeNode], this function does not set the container
// field of the head node, as it is always nil.
func InitializeList[T any](l *List[T]) {
	l.head.lazyInit()
}

// Empty returns true if the list has no elements.
func (l *List[T]) Empty() bool {
	return l.head.Unlinked()
}

// Len counts and returns the number of elements of the list.
func (l *List[T]) Len() int {
	var c int
	for n := l.head.next; n != nil && n != &l.head; n = n.next {
		c++
	}
	return c
}

// Append appends an element to the end of the list.
//
// The element e needs a Node[T] field for each list it is a member of.
func (l *List[T]) Append(cn containedNode[T]) {
	l.head.linkBefore(cn.n)
}

// Prepend prepends an element to the start of the list.
func (l *List[T]) Prepend(cn containedNode[T]) {
	l.head.linkAfter(cn.n)
}

// FirstToLast returns an iterator over elements of the list from first to last.
//
// It is safe to call [Node.Unlink] on the node that participates in the list.
// Iteration always advances through the original chain of nodes.
func (l *List[T]) FirstToLast() Seq[*T] {
	return func(yield func(*T) bool) {
		var next *Node[T]
		for n := l.head.next; n != nil && n != &l.head; n = next {
			next = n.next
			if !yield(n.container) {
				return
			}
		}
	}
}

// LastToFirst returns an iterator over elements of the list from last to first.
//
// It is safe to call [Node.Unlink] on the node that participates in the list.
// Iteration always advances through the original chain of nodes.
func (l *List[T]) LastToFirst() Seq[*T] {
	return func(yield func(*T) bool) {
		var prev *Node[T]
		for n := l.head.prev; n != nil && n != &l.head; n = prev {
			prev = n.prev
			if !yield(n.container) {
				return
			}
		}
	}
}

// HeadlessListPointerer is an interface that types must implement to provide
// access to their embedded HeadlessList[T].
//
// This is used by [ContainedHeadlessList].
type HeadlessListPointerer[T any] interface {
	HeadlessListPointer(*T) *HeadlessList[T]
}

// InitializeHeadlessList stores the pointer [c] inside the node container field.
func InitializeHeadlessList[HLP HeadlessListPointerer[T], T any](c *T) *HeadlessList[T] {
	if c == nil {
		panic("cannot initialize HeadlessList: container is nil")
	}

	var helper HLP
	n := helper.HeadlessListPointer(c)
	if n == nil {
		panic("cannot initialize HeadlessList: node pointer is nil (computed by HeadlessListPointer)")
	}

	n.container = c
	return n
}

// ContainedHeadlessList returns a private type that ensures node container
// pointer is initialized.
//
// The returned value is suitable for use with [HeadlessList.LinkAfter] and
// [HeadlessList.LinkBefore].
func ContainedHeadlessList[HLP HeadlessListPointerer[T], T any](c *T) containedNode[T] {
	return containedNode[T]{n: (*Node[T])(InitializeHeadlessList[HLP, T](c))}
}

// HeadlessList is like [List] but without a dedicated head node.
//
// The list is always circular and is shared equally by all the nodes.  In
// absence of a dedicated head node, there is no specific start or end.
//
// A zero value of a headless acts as if it were pointing to itself.  A
// headless list is thus never empty.
//
// Note that while [HeadlessList] is simply a [Node], only the usage pattern
// enforced by the former allows for type-safe behavior. Since [Node] is shared
// with [List], it is not safe to use [Node] directly as the head element is
// attached at a different offset than all the other elements.
type HeadlessList[T any] Node[T]

// Len counts the number of elements in the list.
//
// The zero value of a headless has the length of one.
func (l *HeadlessList[T]) Len() int {
	var c int

	var next *Node[T]
	start := (*Node[T])(l)
	for node := start; node != nil; node = next {
		next = node.next
		c++
		if next == start {
			break
		}
	}
	return c
}

// LinkBefore links the node within element [e] before the node embedded in [l].
func (l *HeadlessList[T]) LinkBefore(cn containedNode[T]) *HeadlessList[T] {
	(*Node[T])(l).linkBefore(cn.n)
	return l
}

// LinkAfter links the node within element [e] after the node embedded in [l].
func (l *HeadlessList[T]) LinkAfter(cn containedNode[T]) *HeadlessList[T] {
	(*Node[T])(l).linkAfter(cn.n)
	return l
}

// Unlink removes the node embedded in [l] from the list.
func (l *HeadlessList[T]) Unlink() {
	(*Node[T])(l).Unlink()
}

// Forward returns an iterator over elements of the list in the forward direction.
//
// It is safe to call [HeadlessList.Unlink] on the node that participates in
// the list. Iteration always advances through the original chain of nodes.
//
// A zero value list always visits itself.
func (l *HeadlessList[T]) Forward() Seq[*T] {
	return func(yield func(*T) bool) {
		var next *Node[T]
		start := (*Node[T])(l)
		for node := start; node != nil; node = next {
			next = node.next
			if !yield(node.container) || next == start {
				return
			}
		}
	}
}

// Backward returns an iterator over elements of the list in the backward
// direction.
//
// It is safe to call [HeadlessList.Unlink] on the node that participates in the
// list. Iteration always advances through the original chain of nodes.
//
// A zero value list always visits itself.
func (l *HeadlessList[T]) Backward() Seq[*T] {
	start := (*Node[T])(l)
	return func(yield func(*T) bool) {
		var prev *Node[T]
		for node := start; node != nil; node = prev {
			prev = node.prev
			if !yield(node.container) || prev == start {
				return
			}
		}
	}
}
