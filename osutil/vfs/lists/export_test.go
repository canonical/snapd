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

package lists

func (n *Node[T]) Next() *Node[T] { return n.next }
func (n *Node[T]) Prev() *Node[T] { return n.prev }
func (n *Node[T]) Container() *T  { return n.container }
func (n *Node[T]) LazyInit()      { n.lazyInit() }
func (l *List[T]) Head() *Node[T] { return &l.head }
func (l *List[T]) Next() *Node[T] { return l.head.next }
func (l *List[T]) Prev() *Node[T] { return l.head.prev }
func (l *List[T]) LazyInit()      { l.head.lazyInit() }

// At returns the Nth element of the list.
//
// Positive indices iterate first-to-last, with 0 being the first visited element.
// Negative indices iterate last-to-first, with -1 being the first visited element.
func (l *List[T]) At(n int) (e *T) {
	var idx int
	if n >= 0 {
		l.FirstToLast()(func(el *T) bool {
			if idx == n {
				e = el
				return false
			}
			idx++
			return true
		})
	} else {
		l.LastToFirst()(func(el *T) bool {
			idx--
			if idx == n {
				e = el
				return false
			}
			return true
		})
	}
	return e
}

// HeadContainer returns the container of the head element of the list.
func (l *List[T]) HeadContainer() *T {
	return l.head.container
}
