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
	"unsafe"

	"github.com/snapcore/snapd/osutil/vfs/lists"
)

// Skill represents a skill in a tree-like structure.
type Skill struct {
	Name      string
	parent    *Skill
	children  lists.List[Skill, skillChildNode]
	childNode lists.Node[Skill]
}

// String returns the name of the skill.
func (s *Skill) String() string { return s.Name }

// AddChild adds a new child skill to the current skill.
func (s *Skill) AddChild(name string) *Skill {
	child := &Skill{Name: name, parent: s}
	s.children.Append(child)
	return child
}

type skillChildNode struct{}
type skillTree struct{}

func (skillChildNode) Offset(s *Skill) uintptr                          { return unsafe.Offsetof(s.childNode) }
func (skillTree) ChildList(s *Skill) *lists.List[Skill, skillChildNode] { return &s.children }
func (skillTree) ChildNode(s *Skill) *lists.Node[Skill]                 { return &s.childNode }
func (skillTree) Parent(s *Skill) *Skill                                { return s.parent }

func collectSkills(start *Skill) (order []string) {
	lists.DepthFirstSearch[skillTree](start)(func(s *Skill) bool {
		order = append(order, s.Name)
		return true
	})

	return order
}

func TestDepthFirstSearch(t *testing.T) {
	var (
		root      = Skill{Name: "root"}
		fireMagic = root.AddChild("fire")
		_         = fireMagic.AddChild("fire bolt")
		_         = fireMagic.AddChild("fire ball")
		iceMagic  = root.AddChild("ice")
		iceShard  = iceMagic.AddChild("ice shard")
	)

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
