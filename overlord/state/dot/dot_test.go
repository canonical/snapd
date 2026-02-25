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
	"os"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/state/dot"
	"github.com/snapcore/snapd/testutil"
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

	c.Check(g.String(), Equals, strings.TrimSpace(`
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
label=<<b>1-chg - TestWriteDotTo</b>>; labelloc=top; fontsize=24
subgraph "cluster[0]" {
label=<<b>Tasks on lanes: [0]</b>>; fontsize=18
  "d"
}
subgraph "cluster[1]" {
label=<<b>Tasks on lanes: [1]</b>>; fontsize=18
  "a:one"
  "a:two"
}
subgraph "cluster[1 2]" {
label=<<b>Tasks on lanes: [1 2]</b>>; fontsize=18
  "b"
}
subgraph "cluster[2]" {
label=<<b>Tasks on lanes: [2]</b>>; fontsize=18
  "c:three"
}
"a:one" -> "a:two"
"a:one" -> "b" [style=bold]
"a:two" -> "b" [style=bold]
"b" -> "c:three" [style=bold]
}
`)
}

func (s *changeGraphSuite) TestDot(c *C) {
	s.chg.State().Lock()
	defer s.chg.State().Unlock()
	g, err := dot.NewChangeGraph(s.chg, taskLabel, "TestDot")
	c.Assert(err, IsNil)
	c.Check(g.Dot(), Equals, `digraph {
label=<<b>1-chg - TestDot</b>>; labelloc=top; fontsize=24
subgraph "cluster[0]" {
label=<<b>Tasks on lanes: [0]</b>>; fontsize=18
  "d"
}
subgraph "cluster[1]" {
label=<<b>Tasks on lanes: [1]</b>>; fontsize=18
  "a:one"
  "a:two"
}
subgraph "cluster[1 2]" {
label=<<b>Tasks on lanes: [1 2]</b>>; fontsize=18
  "b"
}
subgraph "cluster[2]" {
label=<<b>Tasks on lanes: [2]</b>>; fontsize=18
  "c:three"
}
"a:one" -> "a:two"
"a:one" -> "b" [style=bold]
"a:two" -> "b" [style=bold]
"b" -> "c:three" [style=bold]
}
`)
}

func (s *changeGraphSuite) TestExport(c *C) {
	// just write the stdin of the command to the filename passed to "dot"
	mock := testutil.MockCommand(c, "dot", `cat > "${2#-o}"`)
	defer mock.Restore()

	st := s.chg.State()
	st.Lock()
	defer st.Unlock()

	g, err := dot.NewChangeGraph(s.chg, taskLabel, "TestExport")
	c.Assert(err, IsNil)

	svg, err := g.Export()
	c.Assert(err, IsNil)
	defer os.Remove(svg)

	c.Assert(mock.Calls(), HasLen, 1)
	c.Check(mock.Calls()[0], DeepEquals, []string{"dot", "-Tsvg", "-o" + svg})

	graphSVG, err := os.ReadFile(svg)
	c.Assert(err, IsNil)
	c.Check(strings.Contains(string(graphSVG), "digraph {"), Equals, true)
	c.Check(strings.Contains(string(graphSVG), "TestExport"), Equals, true)
}
