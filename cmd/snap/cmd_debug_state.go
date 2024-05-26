// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/ifacestate/schema"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/strutil"
)

type cmdDebugState struct {
	timeMixin

	Changes  bool   `long:"changes"`
	TaskID   string `long:"task"`
	ChangeID string `long:"change"`
	Check    bool   `long:"check"`

	Connections bool   `long:"connections"`
	Connection  string `long:"connection"`

	IsSeeded bool `long:"is-seeded"`

	// flags for --change=N output
	DotOutput bool `long:"dot"` // XXX: mildly useful (too crowded in many cases), but let's have it just in case
	// When inspecting errors/undone tasks, those in Hold state are usually irrelevant, make it possible to ignore them
	NoHoldState bool `long:"no-hold"`

	Positional struct {
		StateFilePath string `positional-args:"yes" positional-arg-name:"<state-file>"`
	} `positional-args:"yes"`
}

var (
	cmdDebugStateShortHelp = i18n.G("Inspect a snapd state file.")
	cmdDebugStateLongHelp  = i18n.G("Inspect a snapd state file, bypassing snapd API.")
)

type byChangeSpawnTime []*state.Change

func (c byChangeSpawnTime) Len() int           { return len(c) }
func (c byChangeSpawnTime) Swap(i, j int)      { c[i], c[j] = c[j], c[i] }
func (c byChangeSpawnTime) Less(i, j int) bool { return c[i].SpawnTime().Before(c[j].SpawnTime()) }

func loadState(path string) (*state.State, error) {
	if path == "" {
		path = "state.json"
	}
	r := mylog.Check2(os.Open(path))

	defer r.Close()

	return state.ReadState(nil, r)
}

func init() {
	addDebugCommand("state", cmdDebugStateShortHelp, cmdDebugStateLongHelp, func() flags.Commander {
		return &cmdDebugState{}
	}, timeDescs.also(map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"change":      i18n.G("ID of the change to inspect"),
		"task":        i18n.G("ID of the task to inspect"),
		"dot":         i18n.G("Dot (graphviz) output"),
		"no-hold":     i18n.G("Omit tasks in 'Hold' state in the change output"),
		"changes":     i18n.G("List all changes"),
		"connections": i18n.G("List all connections"),
		"connection":  i18n.G("Show details of the matching connections (snap or snap:plug,snap:slot or snap:plug-or-slot"),
		"is-seeded":   i18n.G("Output seeding status (true or false)"),
		"check":       i18n.G("Check change consistency"),
	}), nil)
}

type byLaneAndWaitTaskChain []*state.Task

func (t byLaneAndWaitTaskChain) Len() int      { return len(t) }
func (t byLaneAndWaitTaskChain) Swap(i, j int) { t[i], t[j] = t[j], t[i] }
func (t byLaneAndWaitTaskChain) Less(i, j int) bool {
	if t[i].ID() == t[j].ID() {
		return false
	}
	// cover the typical case (just one lane), and order by first lane
	if t[i].Lanes()[0] == t[j].Lanes()[0] {
		seenTasks := make(map[string]bool)
		return t.waitChainSearch(t[i], t[j], seenTasks)
	}
	return t[i].Lanes()[0] < t[j].Lanes()[0]
}

func (t *byLaneAndWaitTaskChain) waitChainSearch(startT, searchT *state.Task, seenTasks map[string]bool) bool {
	if seenTasks[startT.ID()] {
		return false
	}
	seenTasks[startT.ID()] = true
	for _, cand := range startT.HaltTasks() {
		if cand == searchT {
			return true
		}
		if t.waitChainSearch(cand, searchT, seenTasks) {
			return true
		}
	}

	return false
}

func (c *cmdDebugState) writeDotOutput(st *state.State, changeID string) error {
	st.Lock()
	defer st.Unlock()

	chg := st.Change(changeID)
	if chg == nil {
		return fmt.Errorf("no such change: %s", changeID)
	}

	fmt.Fprintf(Stdout, "digraph D{\n")
	tasks := chg.Tasks()
	for _, t := range tasks {
		if c.NoHoldState && t.Status() == state.HoldStatus {
			continue
		}
		fmt.Fprintf(Stdout, "  %s [label=%q];\n", t.ID(), t.Kind())
		for _, wt := range t.WaitTasks() {
			if c.NoHoldState && wt.Status() == state.HoldStatus {
				continue
			}
			fmt.Fprintf(Stdout, "  %s -> %s;\n", t.ID(), wt.ID())
		}
	}
	fmt.Fprintf(Stdout, "}\n")

	return nil
}

func (c *cmdDebugState) showTasks(st *state.State, changeID string) error {
	st.Lock()
	defer st.Unlock()

	chg := st.Change(changeID)
	if chg == nil {
		return fmt.Errorf("no such change: %s", changeID)
	}

	tasks := chg.Tasks()
	sort.Sort(byLaneAndWaitTaskChain(tasks))

	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	fmt.Fprintf(w, "Lanes\tID\tStatus\tSpawn\tReady\tKind\tSummary\n")
	for _, t := range tasks {
		if c.NoHoldState && t.Status() == state.HoldStatus {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			strutil.IntsToCommaSeparated(t.Lanes()),
			t.ID(),
			t.Status().String(),
			c.fmtTime(t.SpawnTime()),
			c.fmtTime(t.ReadyTime()),
			t.Kind(),
			t.Summary())
	}

	w.Flush()

	for _, t := range tasks {
		logs := t.Log()
		if len(logs) > 0 {
			fmt.Fprintf(Stdout, "---\n")
			fmt.Fprintf(Stdout, "%s %s\n", t.ID(), t.Summary())
			for _, log := range logs {
				fmt.Fprintf(Stdout, "  %s\n", log)
			}
		}
	}

	return nil
}

func (c *cmdDebugState) checkTasks(st *state.State, changeID string) error {
	st.Lock()
	defer st.Unlock()

	showAtMostTasks := 3
	formatAtMostTaskIDs := func(tasks []*state.Task) string {
		var b strings.Builder
		b.WriteRune('[')
		atMostTasks := tasks
		trimmed := false
		if len(atMostTasks) > showAtMostTasks {
			atMostTasks = tasks[:showAtMostTasks]
			trimmed = true
		}
		for i, t := range atMostTasks {
			b.WriteString(t.ID())
			if i < len(atMostTasks)-1 {
				b.WriteRune(',')
			}
		}
		if trimmed {
			b.WriteString(",...")
		}
		b.WriteRune(']')
		return b.String()
	}

	chg := st.Change(changeID)
	if chg == nil {
		return fmt.Errorf("no such change: %s", changeID)
	}
	mylog.Check(chg.CheckTaskDependencies())

	return nil
}

func (c *cmdDebugState) showChanges(st *state.State) error {
	st.Lock()
	defer st.Unlock()

	changes := st.Changes()
	sort.Sort(byChangeSpawnTime(changes))

	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	fmt.Fprintf(w, "ID\tStatus\tSpawn\tReady\tLabel\tSummary\n")
	for _, chg := range changes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			chg.ID(),
			chg.Status().String(),
			c.fmtTime(chg.SpawnTime()),
			c.fmtTime(chg.ReadyTime()),
			chg.Kind(),
			chg.Summary())
	}
	w.Flush()

	return nil
}

func (c *cmdDebugState) showIsSeeded(st *state.State) error {
	st.Lock()
	defer st.Unlock()

	var isSeeded bool
	mylog.Check(st.Get("seeded", &isSeeded))
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}
	fmt.Fprintf(Stdout, "%v\n", isSeeded)

	return nil
}

type connectionInfo struct {
	PlugSnap string
	PlugName string
	SlotSnap string
	SlotName string

	schema.ConnState
}

type byPlug []*connectionInfo

func (c byPlug) Len() int      { return len(c) }
func (c byPlug) Swap(i, j int) { c[i], c[j] = c[j], c[i] }
func (c byPlug) Less(i, j int) bool {
	a, b := c[i], c[j]
	return a.PlugSnap < b.PlugSnap || (a.PlugSnap == b.PlugSnap && a.PlugName < b.PlugName)
}

func (c *cmdDebugState) showConnectionDetails(st *state.State, connArg string) error {
	st.Lock()
	defer st.Unlock()

	p := strings.FieldsFunc(connArg, func(r rune) bool {
		return r == ' ' || r == ','
	})

	var plugMatch, slotMatch SnapAndName
	mylog.Check(plugMatch.UnmarshalFlag(p[0]))

	if len(p) > 1 {
		mylog.Check(slotMatch.UnmarshalFlag(p[1]))
	}

	var conns map[string]*schema.ConnState
	if mylog.Check(st.Get("conns", &conns)); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	// sort by connection ID
	connIDs := make([]string, 0, len(conns))
	for connID := range conns {
		connIDs = append(connIDs, connID)
	}
	sort.Strings(connIDs)

	for _, connID := range connIDs {
		connRef := mylog.Check2(interfaces.ParseConnRef(connID))

		refMatch := func(x SnapAndName, y interface{ String() string }) bool {
			parts := strings.Split(y.String(), ":")
			return len(parts) == 2 && x.Snap == parts[0] && x.Name == parts[1]
		}
		plug, slot := connRef.PlugRef, connRef.SlotRef

		switch {
		// command invoked with 'snap:plug,snap:slot'
		case slotMatch.Name != "" && slotMatch.Snap != "" && plugMatch.Snap != "" && plugMatch.Name != "":
			// should match the connection exactly
			if !refMatch(plugMatch, plug) || !refMatch(slotMatch, slot) {
				continue
			}

		// command invoked with 'snap:plug-or-slot'
		case plugMatch.Snap != "" && plugMatch.Name != "" && slotMatch.Snap == "" && slotMatch.Name == "":
			// should match either the connection's slot or plug
			if !refMatch(plugMatch, plug) && !refMatch(plugMatch, slot) {
				continue
			}

		// command invoked with 'snap' only
		case plugMatch.Snap != "" && plugMatch.Name == "" && slotMatch.Snap == "" && slotMatch.Name == "":
			// should match one of the snap names
			if plugMatch.Snap != slot.Snap && plugMatch.Snap != plug.Snap {
				continue
			}

		default:
			return fmt.Errorf("invalid command with connection args: %s", connArg)
		}

		conn := conns[connID]

		// the output of 'debug connection' is yaml
		fmt.Fprintf(Stdout, "id: %s\n", connID)
		out := mylog.Check2(yaml.Marshal(conn))

		fmt.Fprintf(Stdout, "%s\n", out)
	}
	return nil
}

func (c *cmdDebugState) showConnections(st *state.State) error {
	st.Lock()
	defer st.Unlock()

	var conns map[string]*schema.ConnState
	if mylog.Check(st.Get("conns", &conns)); err != nil && !errors.Is(err, state.ErrNoState) {
		return err
	}

	all := make([]*connectionInfo, 0, len(conns))
	for connID, conn := range conns {
		p := strings.Split(connID, " ")
		if len(p) != 2 {
			return fmt.Errorf("cannot parse connection ID %q", connID)
		}
		plug := strings.Split(p[0], ":")
		slot := strings.Split(p[1], ":")

		c := &connectionInfo{
			PlugSnap:  plug[0],
			PlugName:  plug[1],
			SlotSnap:  slot[0],
			SlotName:  slot[1],
			ConnState: *conn,
		}
		all = append(all, c)
	}

	sort.Sort(byPlug(all))

	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	fmt.Fprintf(w, "Interface\tPlug\tSlot\tNotes\n")
	for _, conn := range all {
		var notes []string
		if conn.Auto {
			notes = append(notes, "auto")
		}
		if conn.Undesired {
			notes = append(notes, "undesired")
		}
		if conn.ByGadget {
			notes = append(notes, "by-gadget")
		}
		fmt.Fprintf(w, "%s\t%s:%s\t%s:%s\t%s\n", conn.Interface, conn.PlugSnap, conn.PlugName, conn.SlotSnap, conn.SlotName, strings.Join(notes, ","))
	}
	w.Flush()

	return nil
}

func (c *cmdDebugState) showTask(st *state.State, taskID string) error {
	st.Lock()
	defer st.Unlock()

	task := st.Task(taskID)
	if task == nil {
		return fmt.Errorf("no such task: %s", taskID)
	}

	termWidth, _ := termSize()
	termWidth -= 3
	if termWidth > 100 {
		// any wider than this and it gets hard to read
		termWidth = 100
	}

	// the output of 'debug task' is yaml'ish
	fmt.Fprintf(Stdout, "id: %s\nkind: %s\nsummary: %s\nstatus: %s\n",
		taskID, task.Kind(),
		task.Summary(),
		task.Status().String())
	log := task.Log()
	if len(log) > 0 {
		fmt.Fprintf(Stdout, "log: |\n")
		for _, msg := range log {
			mylog.Check(strutil.WordWrapPadded(Stdout, []rune(msg), "  ", termWidth))
		}
		fmt.Fprintln(Stdout)
	}

	fmt.Fprintf(Stdout, "halt-tasks:")
	if len(task.HaltTasks()) == 0 {
		fmt.Fprintln(Stdout, " []")
	} else {
		fmt.Fprintln(Stdout)
		for _, ht := range task.HaltTasks() {
			fmt.Fprintf(Stdout, " - %s (%s)\n", ht.Kind(), ht.ID())
		}
	}

	return nil
}

func (c *cmdDebugState) Execute(args []string) error {
	st := mylog.Check2(loadState(c.Positional.StateFilePath))

	// check valid combinations of args
	var cmds []string
	if c.Changes {
		cmds = append(cmds, "--changes")
	}
	if c.ChangeID != "" {
		cmds = append(cmds, "--change=")
	}
	if c.TaskID != "" {
		cmds = append(cmds, "--task=")
	}
	if c.IsSeeded {
		cmds = append(cmds, "--is-seeded")
	}
	if c.Connections {
		cmds = append(cmds, "--connections")
	}
	if len(cmds) > 1 {
		return fmt.Errorf("cannot use %s and %s together", cmds[0], cmds[1])
	}

	if c.IsSeeded {
		return c.showIsSeeded(st)
	}

	if c.DotOutput && c.ChangeID == "" {
		return fmt.Errorf("--dot can only be used with --change=")
	}
	if c.NoHoldState && c.ChangeID == "" {
		return fmt.Errorf("--no-hold can only be used with --change=")
	}
	if c.Check && c.ChangeID == "" {
		return fmt.Errorf("--check can only be used with --change")
	}

	if c.Changes {
		return c.showChanges(st)
	}

	if c.ChangeID != "" {
		_ := mylog.Check2(strconv.ParseInt(c.ChangeID, 0, 64))

		if c.DotOutput {
			return c.writeDotOutput(st, c.ChangeID)
		}
		if c.Check {
			return c.checkTasks(st, c.ChangeID)
		}
		return c.showTasks(st, c.ChangeID)
	}

	if c.TaskID != "" {
		_ := mylog.Check2(strconv.ParseInt(c.TaskID, 0, 64))

		return c.showTask(st, c.TaskID)
	}

	if c.Connections {
		return c.showConnections(st)
	}

	if c.Connection != "" {
		return c.showConnectionDetails(st, c.Connection)
	}

	// show changes by default
	return c.showChanges(st)
}
