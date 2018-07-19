// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"fmt"
	"regexp"
	"sort"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

var shortChangesHelp = i18n.G("List system changes")
var shortTasksHelp = i18n.G("List a change's tasks")
var longChangesHelp = i18n.G(`
The changes command displays a summary of system changes performed recently.
`)
var longTasksHelp = i18n.G(`
The tasks command displays a summary of tasks associated with an individual
change.
`)

type cmdChanges struct {
	timeMixin
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

type cmdTasks struct {
	timeMixin
	changeIDMixin
}

func init() {
	addCommand("changes", shortChangesHelp, longChangesHelp,
		func() flags.Commander { return &cmdChanges{} }, timeDescs, nil)
	addCommand("tasks", shortTasksHelp, longTasksHelp,
		func() flags.Commander { return &cmdTasks{} },
		changeIDMixinOptDesc.also(timeDescs),
		changeIDMixinArgDesc).alias = "change"
}

type changesByTime []*client.Change

func (s changesByTime) Len() int           { return len(s) }
func (s changesByTime) Less(i, j int) bool { return s[i].SpawnTime.Before(s[j].SpawnTime) }
func (s changesByTime) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

var allDigits = regexp.MustCompile(`^[0-9]+$`).MatchString

func queryChanges(cli *client.Client, opts *client.ChangesOptions) ([]*client.Change, error) {
	chgs, err := cli.Changes(opts)
	if err != nil {
		return nil, err
	}
	if err := warnMaintenance(cli); err != nil {
		return nil, err
	}
	return chgs, nil
}

func (c *cmdChanges) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if allDigits(c.Positional.Snap) {
		// TRANSLATORS: the %s is the argument given by the user to 'snap changes'
		return fmt.Errorf(i18n.G(`'snap changes' command expects a snap name, try 'snap tasks %s'`), c.Positional.Snap)
	}

	if c.Positional.Snap == "everything" {
		fmt.Fprintln(Stdout, i18n.G("Yes, yes it does."))
		return nil
	}

	opts := client.ChangesOptions{
		SnapName: c.Positional.Snap,
		Selector: client.ChangesAll,
	}

	cli := Client()
	changes, err := queryChanges(cli, &opts)
	if err != nil {
		return err
	}

	if len(changes) == 0 {
		return fmt.Errorf(i18n.G("no changes found"))
	}

	sort.Sort(changesByTime(changes))

	w := tabWriter()

	fmt.Fprintf(w, i18n.G("ID\tStatus\tSpawn\tReady\tSummary\n"))
	for _, chg := range changes {
		spawnTime := c.fmtTime(chg.SpawnTime)
		readyTime := c.fmtTime(chg.ReadyTime)
		if chg.ReadyTime.IsZero() {
			readyTime = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", chg.ID, chg.Status, spawnTime, readyTime, chg.Summary)
	}

	w.Flush()
	fmt.Fprintln(Stdout)

	return nil
}

func (c *cmdTasks) Execute([]string) error {
	cli := Client()
	chid, err := c.GetChangeID(cli)
	if err != nil {
		return err
	}

	return c.showChange(cli, chid)
}

func queryChange(cli *client.Client, chid string) (*client.Change, error) {
	chg, err := cli.Change(chid)
	if err != nil {
		return nil, err
	}
	if err := warnMaintenance(cli); err != nil {
		return nil, err
	}
	return chg, nil
}

func (c *cmdTasks) showChange(cli *client.Client, chid string) error {
	chg, err := queryChange(cli, chid)
	if err != nil {
		return err
	}

	w := tabWriter()

	fmt.Fprintf(w, i18n.G("Status\tSpawn\tReady\tSummary\n"))
	for _, t := range chg.Tasks {
		spawnTime := c.fmtTime(t.SpawnTime)
		readyTime := c.fmtTime(t.ReadyTime)
		if t.ReadyTime.IsZero() {
			readyTime = "-"
		}
		summary := t.Summary
		if t.Status == "Doing" && t.Progress.Total > 1 {
			summary = fmt.Sprintf("%s (%.2f%%)", summary, float64(t.Progress.Done)/float64(t.Progress.Total)*100.0)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.Status, spawnTime, readyTime, summary)
	}

	w.Flush()

	for _, t := range chg.Tasks {
		if len(t.Log) == 0 {
			continue
		}
		fmt.Fprintln(Stdout)
		fmt.Fprintln(Stdout, line)
		fmt.Fprintln(Stdout, t.Summary)
		fmt.Fprintln(Stdout)
		for _, line := range t.Log {
			fmt.Fprintln(Stdout, line)
		}
	}

	fmt.Fprintln(Stdout)

	return nil
}

const line = "......................................................................"

func warnMaintenance(cli *client.Client) error {
	if maintErr := cli.Maintenance(); maintErr != nil {
		msg, err := errorToCmdMessage("", maintErr, nil)
		if err != nil {
			return err
		}
		fmt.Fprintf(Stderr, "WARNING: %s\n", msg)
	}
	return nil
}
