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

// TreeAspect is an interface that represents a tree-like structure.
//
// TreeAspect allows DepthFirstSearch to traverse the tree of nodes, for as
// long as each node has a list of children (list + node) and a parent pointer.
type TreeAspect[T any] interface {
	// NodePointer returns the list node that represents the given element in
	// its parent's child list.
	NodePointer(*T) *Node[T]
	// ChildList returns the list of children of a given element.
	ChildListPointer(*T) *List[T]
	// Parent returns the parent of the given element.
	ParentPointer(*T) *T
}

// DepthFirstSearch performs a depth-first traversal in pre-order (visit node
// before its children).
//
// The tree aspect of the node is provided by the type parameter [TA]. The type
// parameter [T] is used to represent the element of the tree.
//
// Traversal starts at the start node, then continues through the children
// returning to the parent node when all the children have been visited.
func DepthFirstSearch[TA TreeAspect[T], T any](root *T) Seq[*T] {
	var tree TA

	// This is modeled after next_mnt in the kernel. Note that due to the fact
	// that Go's [iter.Seq] function interface contains an internal loop, we
	// only need one argument (unlike next_mnt).
	return func(yield func(*T) bool) {
		// Yield the initial tree node. This is not in next_mnt, but is in the
		// initial iteration of the loop that uses it, which is just the root
		// of the tree we want to traverse.
		if root == nil || !yield(root) {
			// Note that if the initial iteration returns false then we can
			// naturally get both recursive and non-recursive behavior.
			return
		}

		// Start the traversal through children and their siblings.
		elem := root
		for {
			var nextNode *Node[T]
			if children := tree.ChildListPointer(elem); !children.Empty() {
				// Follow the first child if one exists.
				nextNode = children.head.next
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
					parentElem := tree.ParentPointer(elem)
					if parentElem == nil {
						return
					}

					// If the next sibling of this node is not the list head of
					// the parent element's child list then we found our next
					// sibling.  Note that the break statement takes us out of
					// inner loop where we search for the next sibling.
					childNode := tree.NodePointer(elem)
					parentChildList := tree.ChildListPointer(parentElem)
					if childNode.next != &parentChildList.head {
						nextNode = childNode.next
						break
					}

					// If there's no more siblings at this level then ascend
					// one level up.
					elem = parentElem
				}
			}

			elem = nextNode.container
			// Yield to the caller.
			if !yield(elem) {
				return
			}
		}
	}
}
