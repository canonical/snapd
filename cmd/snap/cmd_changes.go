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
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

var shortChangesHelp = i18n.G("List system changes")
var shortChangeHelp = i18n.G("List a change's tasks")
var longChangesHelp = i18n.G(`
The changes command displays a summary of the recent system changes performed.`)
var longChangeHelp = i18n.G(`
The change command displays a summary of tasks associated to an individual change.`)

type cmdChanges struct {
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

type cmdChange struct {
	Positional struct {
		Id string `positional-arg-name:"<id>" required:"yes"`
	} `positional-args:"yes"`
}

func init() {
	addCommand("changes", shortChangesHelp, longChangesHelp, func() flags.Commander { return &cmdChanges{} })
	addCommand("change", shortChangeHelp, longChangeHelp, func() flags.Commander { return &cmdChange{} })
}

type changesByTime []*client.Change

func (s changesByTime) Len() int           { return len(s) }
func (s changesByTime) Less(i, j int) bool { return s[i].SpawnTime.Before(s[j].SpawnTime) }
func (s changesByTime) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

var allDigits = regexp.MustCompile(`^[0-9]+$`).MatchString

func (c *cmdChanges) Execute([]string) error {

	if allDigits(c.Positional.Snap) {
		return fmt.Errorf(`%s changes command expects a snap name, try: %[1]s change %s`, os.Args[0], c.Positional.Snap)
	}

	if c.Positional.Snap == "everything" {
		fmt.Fprintln(Stdout, "Yes, yes it does.")
		return nil
	}

	opts := client.ChangesOptions{
		SnapName: c.Positional.Snap,
		Selector: client.ChangesAll,
	}

	cli := Client()
	changes, err := cli.Changes(&opts)
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
		spawnTime := chg.SpawnTime.UTC().Format(time.RFC3339)
		readyTime := chg.ReadyTime.UTC().Format(time.RFC3339)
		if chg.ReadyTime.IsZero() {
			readyTime = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", chg.ID, chg.Status, spawnTime, readyTime, chg.Summary)
	}

	w.Flush()
	fmt.Fprintln(Stdout)

	return nil
}

func (c *cmdChange) Execute([]string) error {
	cli := Client()
	chg, err := cli.Change(c.Positional.Id)
	if err != nil {
		return err
	}

	w := tabWriter()

	fmt.Fprintf(w, i18n.G("Status\tSpawn\tReady\tSummary\n"))
	for _, t := range chg.Tasks {
		spawnTime := t.SpawnTime.UTC().Format(time.RFC3339)
		readyTime := t.ReadyTime.UTC().Format(time.RFC3339)
		if t.ReadyTime.IsZero() {
			readyTime = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", t.Status, spawnTime, readyTime, t.Summary)
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
