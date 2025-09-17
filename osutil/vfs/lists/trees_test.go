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

package lists_test

import (
	"slices"
	"testing"

	"github.com/snapcore/snapd/osutil/vfs/lists"
)

// Skill represents a skill in a tree-like structure.
type Skill struct {
	Name      string
	parent    *Skill
	children  lists.List[Skill]
	childNode lists.Node[Skill]
}

func (s *Skill) Init() {
	lists.InitializeNode[skillTree](s)
	lists.InitializeList(&s.children)
}

// String returns the name of the skill.
func (s *Skill) String() string { return s.Name }

// AddChild adds a new child skill to the current skill.
func (s *Skill) AddChild(name string) *Skill {
	child := &Skill{Name: name, parent: s}
	child.Init()
	s.children.Append(lists.ContainedNode[skillTree](child))
	return child
}

type skillTree struct{}

func (skillTree) ParentPointer(s *Skill) *Skill                { return s.parent }
func (skillTree) ChildListPointer(s *Skill) *lists.List[Skill] { return &s.children }
func (skillTree) NodePointer(s *Skill) *lists.Node[Skill]      { return &s.childNode }

func collectSkills(start *Skill) (order []string) {
	lists.DepthFirstSearch[skillTree](start)(func(s *Skill) bool {
		order = append(order, s.Name)
		return true
	})

	return order
}

func TestDepthFirstSearch(t *testing.T) {
	root := Skill{Name: "root"}
	root.Init()

	var (
		fireMagic = root.AddChild("fire")
		fireBolt  = fireMagic.AddChild("fire bolt")
		fireBall  = fireMagic.AddChild("fire ball")
		iceMagic  = root.AddChild("ice")
		iceShard  = iceMagic.AddChild("ice shard")
	)

	t.Run("node-lazy-init", func(t *testing.T) {
		// Ensure that lists were lazy-initialized.
		if root.childNode.Next() == nil {
			t.Error("root.childNode was not lazy-initialized")
		}
		if fireMagic.childNode.Next() == nil {
			t.Error("fireMagic.childNode was not lazy-initialized")
		}
		if fireBall.childNode.Next() == nil {
			t.Error("fireBall.childNode was not lazy-initialized")
		}
		if fireBolt.childNode.Next() == nil {
			t.Error("fireBolt.childNode was not lazy-initialized")
		}
		if iceMagic.childNode.Next() == nil {
			t.Error("iceMagic.childNode was not lazy-initialized")
		}
		if iceShard.childNode.Next() == nil {
			t.Error("iceShard.childNode was not lazy-initialized")
		}
	})

	t.Run("list-lazy-init", func(t *testing.T) {
		// Ensure that lists were lazy-initialized.
		if root.children.Next() == nil {
			t.Error("root.children was not lazy-initialized")
		}
		if fireMagic.children.Next() == nil {
			t.Error("fireMagic.children was not lazy-initialized")
		}
		if fireBall.children.Next() == nil {
			t.Error("fireBall.children was not lazy-initialized")
		}
		if fireBolt.children.Next() == nil {
			t.Error("fireBolt.children was not lazy-initialized")
		}
		if iceMagic.children.Next() == nil {
			t.Error("iceMagic.children was not lazy-initialized")
		}
		if iceShard.children.Next() == nil {
			t.Error("iceShard.children was not lazy-initialized")
		}
	})

	t.Run("container-integrity", func(t *testing.T) {
		// Ensure that nodes point back to their containers.
		if root.childNode.Container() != &root {
			t.Error("root container is nil or incorrect")
		}
		if fireMagic.childNode.Container() != fireMagic {
			t.Error("fireMagic container is nil or incorrect")
		}
		if fireBall.childNode.Container() != fireBall {
			t.Error("fireBall container is nil or incorrect")
		}
		if fireBolt.childNode.Container() != fireBolt {
			t.Error("fireBolt container is nil or incorrect")
		}
		if iceMagic.childNode.Container() != iceMagic {
			t.Error("iceMagic container is nil or incorrect")
		}
		if iceShard.childNode.Container() != iceShard {
			t.Error("iceShard container is nil or incorrect")
		}
	})

	t.Run("parent-pointers", func(t *testing.T) {
		// Ensure the tree structure is as expected.
		if root.parent != nil {
			t.Error("Root should have no parent")
		}
		if fireMagic.parent != &root || iceMagic.parent != &root {
			t.Error("Fire and Ice should have root as parent")
		}
		if iceShard.parent != iceMagic {
			t.Error("Ice shard should have ice as parent")
		}
		if fireBall.parent != fireMagic || fireBolt.parent != fireMagic {
			t.Error("Fire ball and fire bolt should have fire as parent")
		}
	})

	t.Run("childNode pointers", func(t *testing.T) {
		// Ensure that childNode pointers are correct.
		if !root.childNode.Unlinked() {
			t.Error("root.childNode is not unlinked")
		}
	})

	t.Run("children-list", func(t *testing.T) {
		if root.children.Len() != 2 {
			t.Errorf("Root should have 2 children, got %d", root.children.Len())
		}
		if fireMagic.children.Len() != 2 {
			t.Errorf("Fire should have 2 children, got %d", fireMagic.children.Len())
		}
		if fireBall.children.Len() != 0 {
			t.Errorf("Fire ball should have no children, got %d", fireBall.children.Len())
		}
		if fireBolt.children.Len() != 0 {
			t.Errorf("Fire bolt should have no children, got %d", fireBolt.children.Len())
		}
		if iceMagic.children.Len() != 1 {
			t.Errorf("Ice should have 1 child, got %d", iceMagic.children.Len())
		}
		if iceShard.children.Len() != 0 {
			t.Errorf("Ice shard should have no children, got %d", iceShard.children.Len())
		}
		// Ensure that child pointers are correct.
		if root.children.Next() != &fireMagic.childNode ||
			root.children.Next().Next() != &iceMagic.childNode ||
			root.children.Next().Next().Next() != root.children.Head() {
			t.Error("Root's children next chain is corrupted")
		}
		if root.children.Prev() != &iceMagic.childNode ||
			root.children.Prev().Prev() != &fireMagic.childNode ||
			root.children.Prev().Prev().Prev() != root.children.Head() {
			t.Error("Root's children prev chain is corrupted")
		}
		if fireMagic.children.Next() != &fireBolt.childNode ||
			fireMagic.children.Next().Next() != &fireBall.childNode ||
			fireMagic.children.Next().Next().Next() != fireMagic.children.Head() {
			t.Error("Fire's children next chain is corrupted")
		}
		if fireMagic.children.Prev() != &fireBall.childNode ||
			fireMagic.children.Prev().Prev() != &fireBolt.childNode ||
			fireMagic.children.Prev().Prev().Prev() != fireMagic.children.Head() {
			t.Error("Fire's children prev chain is corrupted")
		}
		if iceMagic.children.Next() != &iceShard.childNode ||
			iceMagic.children.Next().Next() != iceMagic.children.Head() {
			t.Error("Ice's children next chain is corrupted")
		}
		if iceMagic.children.Prev() != &iceShard.childNode ||
			iceMagic.children.Prev().Prev() != iceMagic.children.Head() {
			t.Error("Ice's children prev chain is corrupted")
		}
	})

	t.Run("only-first", func(t *testing.T) {
		count := 0
		lists.DepthFirstSearch[skillTree](&root)(func(s *Skill) bool {
			count++
			return false
		})

		if count != 1 {
			t.Errorf("Expected only one iteration, got %d", count)
		}
	})

	t.Run("from-root", func(t *testing.T) {
		order := collectSkills(&root)
		if !slices.Equal(order, []string{"root", "fire", "fire bolt", "fire ball", "ice", "ice shard"}) {
			t.Errorf("Unexpected order of traversal: %v", order)
		}
	})

	t.Run("from-subtree", func(t *testing.T) {
		order := collectSkills(fireMagic)
		if !slices.Equal(order, []string{"fire", "fire bolt", "fire ball"}) {
			t.Errorf("Unexpected order of traversal: %v", order)
		}
	})

	t.Run("leaf-node", func(t *testing.T) {
		order := collectSkills(iceShard)
		if !slices.Equal(order, []string{"ice shard"}) {
			t.Errorf("Unexpected order of traversal: %v", order)
		}
	})
}
