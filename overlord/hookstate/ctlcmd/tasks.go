// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package ctlcmd

import (
	"encoding/json"
	"fmt"

	"io"
	"text/tabwriter"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/timeutil"
)

const ln = "......................................................................"

type tasksCommand struct {
	baseCommand
	Format string `long:"format" required:"false" choice:"json" description:"Output format (supported: json)"`
}

var shortTasksHelp = i18n.G(`Return a list of information associated with all change-ids.`)
var longTasksHelp = i18n.G(`
Used to query the status of all change ids associated with
snapctl commands running in asynchronous mode.

valid options for --format: json

$ snapctl (tasks|change) [--format FORMAT] <change-id>
  0: successfully reported change information, regardless of state of change
  1: any error (invalid change ID, permissions error)
stdout: table of tasks, mirroring "snap tasks|change <change-id>" output
stderr: empty for exit code 0. Contains relevant errors for exit code 1.
`)

func init() {
	// Not using standard alias declaration as that's in *cmdInfo, used by client, we have *commandInfo
	addCommand("change", shortTasksHelp, longTasksHelp, func() command {
		return &tasksCommand{}
	})
	addCommand("tasks", shortTasksHelp, longTasksHelp, func() command {
		return &tasksCommand{}
	})
}

func newTabWriter(output io.Writer) *tabwriter.Writer {
	minWidth := 2
	tabWidth := 2
	padding := 2
	padchar := byte(' ')
	return tabwriter.NewWriter(output, minWidth, tabWidth, padding, padchar, 0)
}

func (c *tasksCommand) Execute(args []string) error {
	ctx, err := c.ensureContext()
	if err != nil {
		return err
	}

	if len(args) != 1 {
		return fmt.Errorf("invalid number of arguments: expected 1, got %d", len(args))
	}

	st := ctx.State()
	st.Lock()
	defer st.Unlock()

	changeID := args[0]
	change, err := getAssociatedChange(ctx, changeID)
	if err != nil {
		return err
	}

	chgInfo := StateChangeToChangeInfo(change)
	clientChg := changeInfoToClientChange(chgInfo)
	tasks := clientChg.Tasks

	if c.Format == "json" {
		if err := json.NewEncoder(c.stdout).Encode(clientChg); err != nil {
			return err
		}
	} else {
		w := newTabWriter(c.stdout)
		fmt.Fprint(w, i18n.G("Status\tSpawn\tReady\tSummary\n"))

		for _, t := range tasks {
			spawnTime := timeutil.Human(t.SpawnTime)
			readyTime := timeutil.Human(t.ReadyTime)
			if t.ReadyTime.IsZero() {
				readyTime = "-"
			}
			summary := t.Summary
			status := t.Status
			pi := t.Progress
			if status == "Doing" && pi.Total > 1 {
				summary = fmt.Sprintf("%s (%.2f%%)", summary, float64(pi.Done)/float64(pi.Total)*100.0)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", status, spawnTime, readyTime, summary)

		}

		w.Flush()

		for _, t := range tasks {
			if len(t.Log) == 0 {
				continue
			}
			fmt.Fprintln(c.stdout)
			fmt.Fprintln(c.stdout, ln)
			fmt.Fprintln(c.stdout, t.Summary)
			fmt.Fprintln(c.stdout)

			for _, line := range t.Log {
				fmt.Fprintln(c.stdout, line)
			}
		}

		fmt.Fprintln(c.stdout)
	}

	return nil
}
