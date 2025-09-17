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

// TreeAspect is an constraint that represents a tree-like structure.
//
// TreeAspect allows DepthFirstSearch to traverse the tree of nodes, for as
// long as each node has a list of children (list + node) and a parent pointer.
type TreeAspect[T any, O Offsetter[T]] interface {
	// ChildList returns the list of children of a given element.
	ChildList(*T) *List[T, O]
	// ChildNode returns the list node of the given element.
	ChildNode(*T) *Node[T]
	// Parent returns the parent of the given element.
	Parent(*T) *T
}

// DepthFirstSearch traverses the tree of nodes in depth-first order.
//
// The tree aspect of the node is provided by the type parameter [TA]. The type
// parameters [T] and [O[ are used to represent the element of the tree, and
// the associated offset within the element of a linkage node used for
// describing children.
//
// Traversal starts at the start node, then continues through the children
// returning to the parent node when all the children have been visited.
func DepthFirstSearch[TA TreeAspect[T, O], T any, O Offsetter[T]](root *T) Seq[*T] {
	var o O
	off := o.Offset(nil)
	var tree TA

	// This is modeled after next_mnt in the kernel. Note that due to the fact
	// that Go's [iter.Seq] function interface contains an internal loop, we
	// only need one argument (unlike next_mnt).
	return func(yield func(*T) bool) {
		// Yield the initial tree node. This is not in next_mnt, but is in the
		// initial iteration of the loop that uses it, which is just the root
		// of the tree we want to traverse.
		if !yield(root) {
			// Note that if the initial iteration returns false then we can
			// naturally get both recursive and non-recursive behavior.
			return
		}

		// Start the traversal through children and their siblings.
		elem := root
		for {
			var nextNode *Node[T]
			if childList := tree.ChildList(elem); !childList.Empty() {
				// Follow the first child if one exists.
				nextNode = childList.head.next
			} else {
				// When we run out of children then we go one level up and
				// traverse the next sibling.
				for {
					// If by following the parent pointer we ever reach the
					// root then we are done.
					if elem == root {
						return
					}

					// Get the parent element.
					parentElem := tree.Parent(elem)
					if parentElem == nil {
						return
					}

					// If the next sibling of this node is not the list head of
					// the parent element's child list then we found our next
					// sibling.  Note that the break statement takes us out of
					// inner loop where we search for the next sibling.
					if childNode := tree.ChildNode(elem); childNode.next != &tree.ChildList(parentElem).head {
						nextNode = childNode.next
						break
					}

					// If there's no more siblings at this level then ascend
					// one level up.
					elem = parentElem
				}
			}

			// Re-construct the node pointer and yield to the caller.
			elem = containerPtr(nextNode, off)
			if !yield(elem) {
				return
			}
		}
	}
}
