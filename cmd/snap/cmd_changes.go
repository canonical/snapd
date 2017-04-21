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
		ID changeID `positional-arg-name:"<id>" required:"yes"`
	} `positional-args:"yes"`
}

type cmdTasks struct {
	LastChangeType string `long:"last"`
	Positional     struct {
		ID changeID `positional-arg-name:"<id>"`
	} `positional-args:"yes"`
}

func init() {
	addCommand("changes", shortChangesHelp, longChangesHelp, func() flags.Commander { return &cmdChanges{} }, nil, nil)
	addCommand("change", shortChangeHelp, longChangeHelp, func() flags.Commander { return &cmdChange{} }, nil, nil).hidden = true
	addCommand("tasks", shortChangeHelp, longChangeHelp, func() flags.Commander { return &cmdTasks{} }, map[string]string{
		"last": "Show last change of given type (install, refresh, remove etc.)",
	}, nil)
}

type changesByTime []*client.Change

func (s changesByTime) Len() int           { return len(s) }
func (s changesByTime) Less(i, j int) bool { return s[i].SpawnTime.Before(s[j].SpawnTime) }
func (s changesByTime) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

var allDigits = regexp.MustCompile(`^[0-9]+$`).MatchString

func (c *cmdChanges) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if allDigits(c.Positional.Snap) {
		// TRANSLATORS: the %s is the argument given by the user to "snap changes"
		return fmt.Errorf(i18n.G(`"snap changes" command expects a snap name, try: "snap tasks %s"`), c.Positional.Snap)
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

func (c *cmdTasks) Execute([]string) error {
	cli := Client()
	var id changeID
	if c.Positional.ID == "" && c.LastChangeType == "" {
		return fmt.Errorf(i18n.G("please provide change ID or type with --last=<type>"))
	}
	if c.Positional.ID != "" && c.LastChangeType != "" {
		return fmt.Errorf(i18n.G("change use ID and type together"))
	}
	if c.LastChangeType != "" {
		kind := c.LastChangeType
		// our internal change types use "-snap" postfix but let user skip it and use short form.
		if kind == "refresh" || kind == "install" || kind == "remove" || kind == "connect" || kind == "disconnect" || kind == "configure" {
			kind += "-snap"
		}
		opts := client.ChangesOptions{
			Selector: client.ChangesAll,
		}
		changes, err := cli.Changes(&opts)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			return fmt.Errorf(i18n.G("no changes found"))
		}
		sort.Sort(sort.Reverse(changesByTime(changes)))
		chg := findChangeByKind(changes, kind)
		if chg == nil {
			return fmt.Errorf(i18n.G("no changes of type %q found"), c.LastChangeType)
		}
		id = changeID(chg.ID)
	} else {
		id = c.Positional.ID
	}

	return showChange(cli, id)
}

func (c *cmdChange) Execute([]string) error {
	cli := Client()
	return showChange(cli, c.Positional.ID)
}

func findChangeByKind(changes []*client.Change, kind string) *client.Change {
	for _, chg := range changes {
		if chg.Kind == kind {
			return chg
		}
	}
	return nil
}

func showChange(cli *client.Client, chid changeID) error {
	chg, err := cli.Change(string(chid))
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
