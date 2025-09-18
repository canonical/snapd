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
	"strings"
	"testing"

	"github.com/snapcore/snapd/osutil/vfs/lists"
)

type Room struct {
	Name      string
	leftRight lists.Node[Room]
}

type viaLeftRight struct{}

func (viaLeftRight) NodePointer(r *Room) *lists.Node[Room] { return &r.leftRight }

func TestNode_Unlink(t *testing.T) {
	t.Run("unlink-zero", func(t *testing.T) {
		var r Room
		r.leftRight.Unlink()
		if !r.leftRight.Unlinked() {
			t.Error("Unlinked Node is not Unlinked")
		}
	})
	t.Run("unlink-appended", func(t *testing.T) {
		var l lists.List[Room]
		var r Room

		l.Append(lists.ContainedNode[viaLeftRight](&r))
		r.leftRight.Unlink()

		if l.Len() != 0 {
			t.Error("List was not empty")
		}
		if !r.leftRight.Unlinked() {
			t.Error("Node was not unlinked")
		}
	})
	t.Run("unlink-unlinked", func(t *testing.T) {
		var l lists.List[Room]
		var r Room

		l.Append(lists.ContainedNode[viaLeftRight](&r))
		r.leftRight.Unlink()
		r.leftRight.Unlink() // This is the actual test.

		if l.Len() != 0 {
			t.Error("List was not empty")
		}
		if !r.leftRight.Unlinked() {
			t.Error("Node was not unlinked")
		}
	})
}

func TestNode_Unlinked(t *testing.T) {
	t.Run("zero-is-unlinked", func(t *testing.T) {
		var r Room
		if !r.leftRight.Unlinked() {
			t.Error("Zero-value Node is not Unlinked")
		}
	})

	t.Run("unlinked-is-unlinked", func(t *testing.T) {
		var r Room
		r.leftRight.Unlink()
		if !r.leftRight.Unlinked() {
			t.Error("Unlinked Node is not Unlinked")
		}
	})
}

func TestList_Len(t *testing.T) {
	var l lists.List[Room]
	for i := 0; i < 100; i++ {
		if v := l.Len(); v != i {
			t.Fatalf("Unexpected length: %d", v)
		}
		l.Append(lists.ContainedNode[viaLeftRight](new(Room)))
	}
}

func TestList_Empty(t *testing.T) {
	t.Run("zero-is-empty", func(t *testing.T) {
		var l lists.List[Room]
		if !l.Empty() {
			t.Error("Zero value list is not empty")
		}
	})

	t.Run("appended-is-not-empty", func(t *testing.T) {
		var l lists.List[Room]
		var r Room
		l.Append(lists.ContainedNode[viaLeftRight](&r))
		if l.Empty() {
			t.Fatal("Appended list is empty")
		}
		r.leftRight.Unlink()
		if !l.Empty() {
			t.Fatal("List not empty after unlinking appended element")
		}
	})

	t.Run("prepended-is-not-empty", func(t *testing.T) {
		var l lists.List[Room]
		var r Room
		l.Prepend(lists.ContainedNode[viaLeftRight](&r))
		if l.Empty() {
			t.Fatal("Non-empty list is empty")
		}
		r.leftRight.Unlink()
		if !l.Empty() {
			t.Fatal("List not empty after unlinking prepended element")
		}
	})
}

func TestList_Append(t *testing.T) {
	var l lists.List[Room]

	l.Append(lists.ContainedNode[viaLeftRight](&Room{Name: "courtyard"}))
	l.Append(lists.ContainedNode[viaLeftRight](&Room{Name: "hallway"}))

	if l.HeadContainer() != nil {
		t.Error("List.head.container must be nil")
	}

	if r := l.At(0); r == nil || r.Name != "courtyard" {
		t.Fatalf("Unexpected room: %v", r)
	}
	if r := l.At(1); r == nil || r.Name != "hallway" {
		t.Fatalf("Unexpected room: %v", r)
	}
	if r := l.At(-1); r == nil || r.Name != "hallway" {
		t.Fatalf("Unexpected room: %v", r)
	}
	if r := l.At(-2); r == nil || r.Name != "courtyard" {
		t.Fatalf("Unexpected room: %v", r)
	}
}

func TestList_Prepend(t *testing.T) {
	var l lists.List[Room]

	l.Prepend(lists.ContainedNode[viaLeftRight](&Room{Name: "courtyard"}))
	l.Prepend(lists.ContainedNode[viaLeftRight](&Room{Name: "hallway"}))

	if l.HeadContainer() != nil {
		t.Error("List.head.container must be nil")
	}

	if r := l.At(0); r == nil || r.Name != "hallway" {
		t.Fatalf("Unexpected room: %v", r)
	}
	if r := l.At(1); r == nil || r.Name != "courtyard" {
		t.Fatalf("Unexpected room: %v", r)
	}
	if r := l.At(-1); r == nil || r.Name != "courtyard" {
		t.Fatalf("Unexpected room: %v", r)
	}
	if r := l.At(-2); r == nil || r.Name != "hallway" {
		t.Fatalf("Unexpected room: %v", r)
	}
}

func TestList_FirstToLast(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var l lists.List[Room]
		n := 0
		l.FirstToLast()(func(r *Room) bool {
			n++
			return true
		})
		if n != 0 {
			t.Errorf("Unexpected number of iterations: %d", n)
		}
	})

	t.Run("appended", func(t *testing.T) {
		var l lists.List[Room]
		var r1, r2, r3 Room

		l.Append(lists.ContainedNode[viaLeftRight](&r1))
		l.Append(lists.ContainedNode[viaLeftRight](&r2))
		l.Append(lists.ContainedNode[viaLeftRight](&r3))

		n := 0
		l.FirstToLast()(func(r *Room) bool {
			var ok bool
			switch n {
			case 0:
				ok = r == &r1
			case 1:
				ok = r == &r2
			case 2:
				ok = r == &r3
			}
			if !ok {
				t.Errorf("Unexpected room at #%d", n)
			}

			n++
			return true
		})
		if n != 3 {
			t.Errorf("Unexpected number of iterations: %d", n)
		}
	})

	t.Run("prepended", func(t *testing.T) {
		var l lists.List[Room]
		var r1, r2, r3 Room

		l.Prepend(lists.ContainedNode[viaLeftRight](&r1))
		l.Prepend(lists.ContainedNode[viaLeftRight](&r2))
		l.Prepend(lists.ContainedNode[viaLeftRight](&r3))

		n := 0
		l.FirstToLast()(func(r *Room) bool {
			var ok bool
			switch n {
			case 0:
				ok = r == &r3
			case 1:
				ok = r == &r2
			case 2:
				ok = r == &r1
			}
			if !ok {
				t.Errorf("Unexpected room at #%d", n)
			}

			n++
			return true
		})
		if n != 3 {
			t.Errorf("Unexpected number of iterations: %d", n)
		}
	})

	t.Run("calling-unlink", func(t *testing.T) {
		var l lists.List[Room]
		var r1, r2, r3 Room

		l.Append(lists.ContainedNode[viaLeftRight](&r1))
		l.Append(lists.ContainedNode[viaLeftRight](&r2))
		l.Append(lists.ContainedNode[viaLeftRight](&r3))

		l.FirstToLast()(func(r *Room) bool {
			r.leftRight.Unlink()
			return true
		})

		if l.Len() != 0 {
			t.Error("Unlinked list is not empty")
		}
		if !r1.leftRight.Unlinked() {
			t.Error("Node of r1 is not unlinked")
		}
		if !r2.leftRight.Unlinked() {
			t.Error("Node of r1 is not unlinked")
		}
		if !r3.leftRight.Unlinked() {
			t.Error("Node of r1 is not unlinked")
		}
	})
}

func Test_LastToFirst(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		var l lists.List[Room]
		n := 0
		l.FirstToLast()(func(r *Room) bool {
			n++
			return true
		})
		if n != 0 {
			t.Fatalf("Unexpected number of iterations: %d", n)
		}
	})

	t.Run("appended", func(t *testing.T) {
		var l lists.List[Room]
		var r1, r2, r3 Room

		l.Append(lists.ContainedNode[viaLeftRight](&r1))
		l.Append(lists.ContainedNode[viaLeftRight](&r2))
		l.Append(lists.ContainedNode[viaLeftRight](&r3))

		n := 0
		l.LastToFirst()(func(r *Room) bool {
			var ok bool
			switch n {
			case 0:
				ok = r == &r3
			case 1:
				ok = r == &r2
			case 2:
				ok = r == &r1
			}
			if !ok {
				t.Errorf("Unexpected room at #%d", n)
			}

			n++
			return true
		})
		if n != 3 {
			t.Errorf("Unexpected number of iterations: %d", n)
		}
	})

	t.Run("prepended", func(t *testing.T) {
		var l lists.List[Room]
		var r1, r2, r3 Room

		l.Prepend(lists.ContainedNode[viaLeftRight](&r1))
		l.Prepend(lists.ContainedNode[viaLeftRight](&r2))
		l.Prepend(lists.ContainedNode[viaLeftRight](&r3))

		n := 0
		l.LastToFirst()(func(r *Room) bool {
			var ok bool
			switch n {
			case 0:
				ok = r == &r1
			case 1:
				ok = r == &r2
			case 2:
				ok = r == &r3
			}
			if !ok {
				t.Errorf("Unexpected room at #%d: %s", n, r.Name)
			}

			n++
			return true
		})
		if n != 3 {
			t.Errorf("Unexpected number of iterations: %d", n)
		}
	})

	t.Run("calling-unlink", func(t *testing.T) {
		var l lists.List[Room]
		var r1, r2, r3 Room

		l.Append(lists.ContainedNode[viaLeftRight](&r1))
		l.Append(lists.ContainedNode[viaLeftRight](&r2))
		l.Append(lists.ContainedNode[viaLeftRight](&r3))

		l.LastToFirst()(func(r *Room) bool {
			r.leftRight.Unlink()
			return true
		})

		if l.Len() != 0 {
			t.Error("Unlinked list is not empty")
		}
		if !r1.leftRight.Unlinked() {
			t.Error("Node of r1 is not unlinked")
		}
		if !r2.leftRight.Unlinked() {
			t.Error("Node of r1 is not unlinked")
		}
		if !r3.leftRight.Unlinked() {
			t.Error("Node of r1 is not unlinked")
		}
	})
}

type Word struct {
	Name  string
	Chain lists.HeadlessList[Word]
}
type viaWords struct{}

func (viaWords) HeadlessListPointer(w *Word) *lists.HeadlessList[Word] { return &w.Chain }

func WordsForward(w *Word) string {
	var sb strings.Builder
	w.Chain.Forward()(func(w *Word) bool {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(w.Name)
		return true
	})
	return sb.String()
}

func WordsBackward(w *Word) string {
	var sb strings.Builder
	w.Chain.Backward()(func(w *Word) bool {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(w.Name)
		return true
	})
	return sb.String()
}

func TestHeadlessList_Smoke(t *testing.T) {
	var (
		marry  = Word{Name: "Marry"}
		had    = Word{Name: "had"}
		a      = Word{Name: "a"}
		little = Word{Name: "little"}
		lamb   = Word{Name: "lamb"}
	)

	if v := marry.Chain.Len(); v != 1 {
		t.Errorf("Unexpected length: %d", v)
	}

	marry.Chain.LinkAfter(lists.ContainedHeadlessList[viaWords](&marry))

	if v := marry.Chain.Len(); v != 1 {
		t.Errorf("Unexpected length: %d", v)
	}

	if v := WordsForward(&marry); v != "Marry" {
		t.Errorf("Unexpected sentence: %q", v)
	}

	marry.Chain.LinkBefore(lists.ContainedHeadlessList[viaWords](&marry))

	if v := marry.Chain.Len(); v != 1 {
		t.Errorf("Unexpected length: %d", v)
	}

	if v := WordsForward(&marry); v != "Marry" {
		t.Errorf("Unexpected sentence: %q", v)
	}

	marry.Chain.LinkBefore(
		lists.ContainedHeadlessList[viaWords](&had)).LinkBefore(
		lists.ContainedHeadlessList[viaWords](&a)).LinkBefore(
		lists.ContainedHeadlessList[viaWords](&little)).LinkBefore(
		lists.ContainedHeadlessList[viaWords](&lamb))

	if v := marry.Chain.Len(); v != 5 {
		t.Errorf("Unexpected length: %d", v)
	}
	if v := a.Chain.Len(); v != 5 {
		t.Errorf("Unexpected length: %d", v)
	}
	if v := lamb.Chain.Len(); v != 5 {
		t.Errorf("Unexpected length: %d", v)
	}

	if v := WordsForward(&marry); v != "Marry had a little lamb" {
		t.Errorf("Unexpected sentence: %q", v)
	}
	if v := WordsForward(&a); v != "a little lamb Marry had" {
		t.Errorf("Unexpected sentence: %q", v)
	}
	if v := WordsBackward(&lamb); v != "lamb little a had Marry" {
		t.Errorf("Unexpected sentence: %q", v)
	}
}
