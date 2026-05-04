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
	"time"

	"io"
	"text/tabwriter"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/timeutil"
)

type tasksCommand struct {
	baseCommand
	Format string `long:"format" required:"false" description:"Output format (json)"`
}

var shortTasksHelp = i18n.G(`Return a list of information associated with all change-ids.`)
var longTasksHelp = i18n.G(`
Used to query the status of all change ids associated with
snapctl commands running in asynchronous mode.

$ snapctl (tasks/change) [--format FORMAT]
  0: successfully reported change information, regardless of state of change
  1: any error (invalid change ID, permissions error)
stdout: table of tasks, mirroring "snap changes <change-id>" output
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

func (c *tasksCommand) newTabWriter(output io.Writer) *tabwriter.Writer {
	minWidth := 2
	tabWidth := 2
	padding := 2
	padchar := byte(' ')
	return tabwriter.NewWriter(output, minWidth, tabWidth, padding, padchar, 0)
}

func fmtTime(t time.Time, abs bool) string {
	if abs {
		return t.Format(time.RFC3339)
	}
	return timeutil.Human(t)
}

func (c *tasksCommand) Execute(args []string) error {
	ctx, err := c.ensureContext()
	if err != nil {
		return err
	}

	if len(args) != 1 {
		return fmt.Errorf("invalid number of arguments: expected 1, got %d", len(args))
	}

	changeID := args[0]

	change, err := getAssociatedChange(ctx, changeID)

	if err != nil {
		return err
	}

	st := ctx.State()

	if c.Format == "json" {
		st.Lock()
		chgInfo := StateChangeToChangeInfo(change)
		st.Unlock()
		data, err := json.Marshal(chgInfo)
		if err != nil {
			return err
		}
		fmt.Fprint(c.stdout, string(data))
	} else {
		w := c.newTabWriter(c.stdout)

		fmt.Fprint(w, i18n.G("ID\tStatus\tSpawn\tReady\tSummary\n"))

		st.Lock()
		for _, t := range change.Tasks() {
			spawnTime := fmtTime(t.SpawnTime(), false)
			readyTime := fmtTime(t.ReadyTime(), false)
			if t.ReadyTime().IsZero() {
				readyTime = "-"
			}
			summary := t.Summary()
			status := t.Status()
			_, done, total := t.Progress()
			if status == state.DoingStatus && total > 1 {
				summary = fmt.Sprintf("%s (%.2f%%)", summary, float64(done)/float64(total)*100.0)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID(), status.String(), spawnTime, readyTime, summary)
		}
		st.Unlock()

		w.Flush()
		fmt.Fprintln(c.stdout)
	}

	return nil
}
