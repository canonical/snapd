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

// Package dot allows to build graphviz dot representations of the task
// dependency graph of state changes. Assuming that some care has been taken to
// avoid non-determinism building the changes, these representations can also
// be used for canonical comparison in tests.
package dot

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/state"
)

// gfrag represents a fragment line of graph definition
type gfrag interface {
	appendTo(w io.Writer, withAttrs bool) error
}

type gcluster struct {
	label string
}

func (gclu gcluster) appendTo(w io.Writer, withAttrs bool) error {
	_, err := fmt.Fprintf(w, "subgraph \"cluster%s\" {\n", gclu.label)
	if err != nil {
		return err
	}
	if withAttrs {
		_, err := fmt.Fprintf(w, "label=<<b>Tasks on lanes: %s</b>>; fontsize=18\n", gclu.label)
		if err != nil {
			return err
		}
	}
	return nil
}

type gclusterEnd struct{}

func (gclusterEnd) appendTo(w io.Writer, withAttrs bool) error {
	_, err := io.WriteString(w, "}\n")
	return err
}

type gnode struct {
	name string
}

func (gn gnode) appendTo(w io.Writer, withAttrs bool) error {
	_, err := fmt.Fprintf(w, "  \"%s\"\n", gn.name)
	return err
}

type gedge struct {
	from  string
	to    string
	attrs string
}

func (ge *gedge) appendTo(w io.Writer, withAttrs bool) error {
	_, err := fmt.Fprintf(w, `"%s" -> "%s"`, ge.from, ge.to)
	if err != nil {
		return err
	}
	if withAttrs && ge.attrs != "" {
		_, err := fmt.Fprintf(w, " [%s]\n", ge.attrs)
		return err
	}
	_, err = io.WriteString(w, "\n")
	return err
}

// ChangeGraph holds a graphviz dot representation of a change task dependency graph.
type ChangeGraph struct {
	tags []string
	def  []gfrag
}

// NewChangeGraph builds a new ChangeGraph with a graphviz dot representation
// of the task dependency graph of the given Change. taskLabeler should return
// unique labels for tasks in the change, usually of the form "<task-kind>" for
// unparameterized tasks or "<task-kind>:<params-repr>" for parameterized
// tasks. tag can be used to trace the context of origin of the change, and
// will appear in the graphical representation of the graph.
func NewChangeGraph(chg *state.Change, taskLabeler func(*state.Task) (label string, err error), tag string) (*ChangeGraph, error) {
	tasks := chg.Tasks()
	labels := make(map[*state.Task]string, len(tasks))
	for _, t := range tasks {
		label, err := taskLabeler(t)
		if err != nil {
			return nil, err
		}
		labels[t] = label
	}
	sortTasks(tasks, labels)
	// use cluster subgraphs for tasks in the same lanes
	var clusters [][]int
	clusterTasks := make(map[string][]*state.Task)
	taskToCluster := make(map[*state.Task]string, len(tasks))
	for _, t := range tasks {
		lanes := t.Lanes()
		sort.Ints(lanes)
		clulabel := clusterLabel(lanes)
		incluster, ok := clusterTasks[clulabel]
		if !ok {
			clusters = append(clusters, lanes)
		}
		clusterTasks[clulabel] = append(incluster, t)
		taskToCluster[t] = clulabel
	}
	sort.Slice(clusters, func(i, j int) bool {
		return lanesLess(clusters[i], clusters[j])
	})
	var def []gfrag
	addToDef := func(f gfrag) {
		def = append(def, f)
	}
	for _, clu := range clusters {
		clulabel := clusterLabel(clu)
		addToDef(gcluster{clulabel})
		for _, t := range clusterTasks[clulabel] {
			addToDef(gnode{labels[t]})
		}
		addToDef(gclusterEnd{})
	}
	for _, t := range tasks {
		clu := taskToCluster[t]
		haltTasks := t.HaltTasks()
		sortTasks(haltTasks, labels)
		for _, t2 := range haltTasks {
			attrs := ""
			if taskToCluster[t2] != clu {
				// cross cluster
				attrs = "style=bold"
			}
			addToDef(&gedge{
				from:  labels[t],
				to:    labels[t2],
				attrs: attrs,
			})
		}
	}
	tags := []string{fmt.Sprintf("%s-%s", chg.ID(), chg.Kind())}
	if tag != "" {
		tags = append(tags, tag)
	}
	return &ChangeGraph{
		tags: tags,
		def:  def,
	}, nil
}

// String returns a canonical text for the graphviz dot representation of the
// change dependency graph. This can be used for comparison in tests with some
// care, OTOH it is not a syntactically complete dot file.
func (g *ChangeGraph) String() string {
	buf := new(strings.Builder)
	const withAttrs = false
	for _, f := range g.def {
		f.appendTo(buf, withAttrs)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// Dot generates and returns a complete dot representation of the change
// dependency graph.
func (g *ChangeGraph) Dot() (graphDot string) {
	gbuf := new(bytes.Buffer)
	g.WriteDotTo(gbuf)
	return gbuf.String()
}

// WriteDotTo writes a syntactically complete dot file for representation of
// the change dependency graph to the given io.Writer.
func (g *ChangeGraph) WriteDotTo(w io.Writer) error {
	if _, err := io.WriteString(w, "digraph {\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "label=<<b>%s</b>>; labelloc=top; fontsize=24\n", strings.Join(g.tags, " - ")); err != nil {
		return err
	}
	const withAttrs = true
	for _, f := range g.def {
		if err := f.appendTo(w, withAttrs); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "}\n")
	return err
}

// Export invokes the dot command and writes the graphviz representation of the
// change dependency graph into a temporary SVG file, returning the SVG path.
func (g *ChangeGraph) Export() (svgPath string, err error) {
	f, err := os.CreateTemp("", fmt.Sprintf("%s-*.svg", strings.Join(strings.Fields(strings.Join(g.tags, "-")), "-")))
	if err != nil {
		return "", fmt.Errorf("cannot create .svg file: %v", err)
	}
	svgPath = f.Name()
	f.Close()
	dotCmd := exec.Command("dot", "-Tsvg", "-o"+svgPath)
	gbuf := new(bytes.Buffer)
	if err := g.WriteDotTo(gbuf); err != nil {
		return "", err
	}
	dotCmd.Stdin = gbuf
	if o, err := dotCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("cannot process dot definition: %v", osutil.OutputErr(o, err))
	}
	return svgPath, nil
}

func clusterLabel(lanes []int) string {
	return fmt.Sprintf("%v", lanes)
}

func lanesLess(lanes1, lanes2 []int) bool {
	n1 := len(lanes1)
	n2 := len(lanes2)
	n := n1
	if n2 < n {
		n = n2
	}
	for i := 0; i < n; i++ {
		if lanes1[i] < lanes2[i] {
			return true
		}
		if lanes1[i] > lanes2[i] {
			return false
		}
	}
	return n1 < n2
}

func sortTasks(tasks []*state.Task, labels map[*state.Task]string) {
	sort.Slice(tasks, func(i, j int) bool {
		return labels[tasks[i]] < labels[tasks[j]]
	})
}
