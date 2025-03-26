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
		_, err := fmt.Fprintf(w, "label=\"%s\"\n", gclu.label)
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
	profile, err := chg.ProfileExecution()
	if err != nil {
		return nil, err
	}
	if profile.UnresolvedTaskCount() != 0 {
		return nil, fmt.Errorf("change %s have unresolved dependencies", chg.ID())
	}

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
			if profile.IsRedundant(t2, t) {
				attrs = "style=dotted, color=grey"
				//continue
			}
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
	tags := []string{chg.Kind()}
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

// WriteDotTo writes a syntactically complete dot file for representation of
// the change dependency graph to the given io.Writer.
func (g *ChangeGraph) WriteDotTo(w io.Writer) error {
	if _, err := io.WriteString(w, "digraph {\n"); err != nil {
		return err
	}
	/*if _, err := fmt.Fprintf(w, "concentrate=true;"); err != nil {
                return err
        }*/
	if _, err := fmt.Fprintf(w, "label=\"%s\"\n", strings.Join(g.tags, " - ")); err != nil {
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

// Show invokes the dot command and uses xdg-open to graphically display the
// graphviz representation of the change dependency graph.
// If provided logfer is used to report errors and information, if nil stderr
// and stdout are used instead.
func (g *ChangeGraph) Show(logfer interface {
	Logf(format string, args ...interface{})
}) *ChangeGraph {
	f, err := os.CreateTemp("", fmt.Sprintf("%s-*.dot", strings.Join(g.tags, "-")))
	fprintfln := func(w io.Writer, format string, args ...interface{}) {
		if logfer != nil {
			logfer.Logf(format, args...)
		} else {
			fmt.Fprintf(w, format+"\n", args...)
		}
	}
	if err != nil {
		fprintfln(os.Stderr, "cannot create .dot file: %v", err)
		return g
	}
	output := f.Name()
	fmt.Printf(">>>> FILE NAME: %s\n", output)
	f.Close()
	dotCmd := exec.Command("dot", "-Tsvg", "-o"+output)
	gbuf := new(bytes.Buffer)
	g.WriteDotTo(gbuf)
	dotCmd.Stdin = gbuf
	if o, err := dotCmd.CombinedOutput(); err != nil {
		if _, ok := err.(*exec.Error); ok {
			return g
		}
		fprintfln(os.Stderr, "cannot process .dot file: %v", osutil.OutputErr(o, err))
		return g
	}
	fprintfln(os.Stdout, "%s => %s", strings.Join(g.tags, " "), output)
	exec.Command("xdg-open", output).Run()
	return g
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
