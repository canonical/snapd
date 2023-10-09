/*
 * Copyright (C) 2023 Canonical Ltd
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

package dot_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/state/dot"
)

func TestDot(t *testing.T) { TestingT(t) }

type changeGraphSuite struct {
	chg *state.Change
}

var _ = Suite(&changeGraphSuite{})

func (s *changeGraphSuite) SetUpTest(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	ta1 := st.NewTask("a", "a1")
	ta1.Set("param", "one")
	ta2 := st.NewTask("a", "a2")
	ta2.Set("param", "two")
	tb := st.NewTask("b", "b")
	tc := st.NewTask("c", "c")
	tc.Set("param", "three")
	td := st.NewTask("d", "d")

	// 1
	l1 := st.NewLane()
	// 2
	l2 := st.NewLane()

	ta1.JoinLane(l1)
	ta2.JoinLane(l1)
	ta2.WaitFor(ta1)
	tb.JoinLane(l1)
	tb.JoinLane(l2)
	tb.WaitFor(ta1)
	tb.WaitFor(ta2)
	tc.JoinLane(l2)
	tc.WaitFor(tb)
	// d waits for nothing else

	chg := st.NewChange("chg", "test change")
	chg.AddTask(ta1)
	chg.AddTask(ta2)
	chg.AddTask(tb)
	chg.AddTask(tc)
	chg.AddTask(td)
	s.chg = chg
}

func taskLabel(t *state.Task) (string, error) {
	label := t.Kind()
	var param string
	err := t.Get("param", &param)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return "", err
	}
	if param != "" {
		label += ":" + param
	}
	return label, nil
}

func (s *changeGraphSuite) TestString(c *C) {
	s.chg.State().Lock()
	defer s.chg.State().Unlock()
	g, err := dot.NewChangeGraph(s.chg, taskLabel, "TestString")
	c.Assert(err, IsNil)

	// XXX remove the show before landing
	c.Check(g.Show(c).String(), Equals, strings.TrimSpace(`
subgraph "cluster[0]" {
  "d"
}
subgraph "cluster[1]" {
  "a:one"
  "a:two"
}
subgraph "cluster[1 2]" {
  "b"
}
subgraph "cluster[2]" {
  "c:three"
}
"a:one" -> "a:two"
"a:one" -> "b"
"a:two" -> "b"
"b" -> "c:three"
`), Commentf("%v", g))
}

func (s *changeGraphSuite) TestWriteDotTo(c *C) {
	s.chg.State().Lock()
	defer s.chg.State().Unlock()
	g, err := dot.NewChangeGraph(s.chg, taskLabel, "TestWriteDotTo")
	c.Assert(err, IsNil)
	b := new(bytes.Buffer)
	err = g.WriteDotTo(b)
	c.Assert(err, IsNil)
	c.Check(b.String(), Equals, `digraph {
label="chg - TestWriteDotTo"
subgraph "cluster[0]" {
label="[0]"
  "d"
}
subgraph "cluster[1]" {
label="[1]"
  "a:one"
  "a:two"
}
subgraph "cluster[1 2]" {
label="[1 2]"
  "b"
}
subgraph "cluster[2]" {
label="[2]"
  "c:three"
}
"a:one" -> "a:two"
"a:one" -> "b" [style=bold]
"a:two" -> "b" [style=bold]
"b" -> "c:three" [style=bold]
}
`)
}
