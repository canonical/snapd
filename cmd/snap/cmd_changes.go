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
	"sort"
	"text/tabwriter"

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"

	"github.com/jessevdk/go-flags"
	"time"
)

var shortChangesHelp = i18n.G("List system changes")
var longChangesHelp = i18n.G(`
The changes command displays a summary of the recent system changes performed.`)

type cmdChanges struct {
	Positional struct {
		Id string `positional-arg-name:"<id>"`
	} `positional-args:"yes"`
}

func init() {
	addCommand("changes", shortChangesHelp, longChangesHelp, func() flags.Commander { return &cmdChanges{} })
}

type changesByTime []*client.Change

func (s changesByTime) Len() int           { return len(s) }
func (s changesByTime) Less(i, j int) bool { return s[i].SpawnTime.Before(s[j].SpawnTime) }
func (s changesByTime) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func (c *cmdChanges) Execute([]string) error {

	if c.Positional.Id != "" {
		return c.showChange(c.Positional.Id)
	}

	cli := Client()
	changes, err := cli.Changes(client.ChangesAll)
	if err != nil {
		return err
	}

	if len(changes) == 0 {
		return fmt.Errorf(i18n.G("no changes found"))
	}

	sort.Sort(changesByTime(changes))

	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, i18n.G("ID\tStatus\tSpawn Time\tReady Time\tSummary\n"))
	for _, chg := range changes {
		spawnTime := chg.SpawnTime.UTC().Format(time.RFC3339)
		readyTime := chg.ReadyTime.UTC().Format(time.RFC3339)
		if chg.ReadyTime.IsZero() {
			readyTime = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", chg.ID, chg.Status, spawnTime, readyTime, chg.Summary)
	}

	return nil
}

func (c *cmdChanges) showChange(id string) error {
	cli := Client()
	chg, err := cli.Change(id)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
	defer w.Flush()

	fmt.Fprintf(w, i18n.G("ID\tStatus\tSpawn Time\tReady Time\tSummary\n"))
	for _, t := range chg.Tasks {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Status, t.Summary)
	}

	return nil
}
