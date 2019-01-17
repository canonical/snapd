// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil/quantity"
)

type cmdWarnings struct {
	clientMixin
	timeMixin
	All     bool `long:"all"`
	Verbose bool `long:"verbose"`
}

type cmdOkay struct{ clientMixin }

var shortWarningsHelp = i18n.G("List warnings")
var longWarningsHelp = i18n.G(`
The warnings command lists the warnings that have been reported to the system.

Once warnings have been listed with 'snap warnings', 'snap okay' may be used to
silence them. A warning that's been silenced in this way will not be listed
again unless it happens again, _and_ a cooldown time has passed.

Warnings expire automatically, and once expired they are forgotten.
`)

var shortOkayHelp = i18n.G("Acknowledge warnings")
var longOkayHelp = i18n.G(`
The okay command acknowledges the warnings listed with 'snap warnings'.

Once acknowledged a warning won't appear again unless it re-occurrs and
sufficient time has passed.
`)

func init() {
	addCommand("warnings", shortWarningsHelp, longWarningsHelp, func() flags.Commander { return &cmdWarnings{} }, timeDescs.also(map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"all": i18n.G("Show all warnings"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"verbose": i18n.G("Show more information"),
	}), nil)
	addCommand("okay", shortOkayHelp, longOkayHelp, func() flags.Commander { return &cmdOkay{} }, nil, nil)
}

func (cmd *cmdWarnings) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	now := time.Now()

	warnings, err := cmd.client.Warnings(client.WarningsOptions{All: cmd.All})
	if err != nil {
		return err
	}
	if len(warnings) == 0 {
		if t, _ := lastWarningTimestamp(); t.IsZero() {
			fmt.Fprintln(Stdout, i18n.G("No warnings."))
		} else {
			fmt.Fprintln(Stdout, i18n.G("No further warnings."))
		}
		return nil
	}

	if err := writeWarningTimestamp(now); err != nil {
		return err
	}

	w := tabWriter()
	if cmd.Verbose {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			i18n.G("First occurrence"),
			i18n.G("Last occurrence"),
			i18n.G("Expires after"),
			i18n.G("Acknowledged"),
			i18n.G("Repeats after"),
			i18n.G("Warning"))
		for _, warning := range warnings {
			lastShown := "-"
			if !warning.LastShown.IsZero() {
				lastShown = cmd.fmtTime(warning.LastShown)
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				cmd.fmtTime(warning.FirstAdded),
				cmd.fmtTime(warning.LastAdded),
				quantity.FormatDuration(warning.ExpireAfter.Seconds()),
				lastShown,
				quantity.FormatDuration(warning.RepeatAfter.Seconds()),
				warning.Message)
		}
	} else {
		fmt.Fprintf(w, "%s\t%s\n", i18n.G("Last occurrence"), i18n.G("Warning"))
		for _, warning := range warnings {
			fmt.Fprintf(w, "%s\t%s\n", cmd.fmtTime(warning.LastAdded), warning.Message)
		}
	}
	w.Flush()

	return nil
}

func (cmd *cmdOkay) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	last, err := lastWarningTimestamp()
	if err != nil {
		return fmt.Errorf("no client-side warning timestamp found: %v", err)
	}

	return cmd.client.Okay(last)
}

const warnFileEnvKey = "SNAPD_LAST_WARNING_TIMESTAMP_FILENAME"

func warnFilename(homedir string) string {
	if fn := os.Getenv(warnFileEnvKey); fn != "" {
		return fn
	}

	return filepath.Join(dirs.GlobalRootDir, homedir, ".snap", "warnings.json")
}

type clientWarningData struct {
	Timestamp time.Time `json:"timestamp"`
}

func writeWarningTimestamp(t time.Time) error {
	user, err := osutil.RealUser()
	if err != nil {
		return err
	}
	uid, gid, err := osutil.UidGid(user)
	if err != nil {
		return err
	}

	filename := warnFilename(user.HomeDir)
	if err := osutil.MkdirAllChown(filepath.Dir(filename), 0700, uid, gid); err != nil {
		return err
	}

	aw, err := osutil.NewAtomicFile(filename, 0600, 0, uid, gid)
	if err != nil {
		return err
	}
	// Cancel once Committed is a NOP :-)
	defer aw.Cancel()

	enc := json.NewEncoder(aw)
	if err := enc.Encode(clientWarningData{Timestamp: t}); err != nil {
		return err
	}

	return aw.Commit()
}

func lastWarningTimestamp() (time.Time, error) {
	user, err := osutil.RealUser()
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot determine real user: %v", err)
	}
	f, err := os.Open(warnFilename(user.HomeDir))
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot open timestamp file: %v", err)

	}
	dec := json.NewDecoder(f)
	var d clientWarningData
	if err := dec.Decode(&d); err != nil {
		return time.Time{}, fmt.Errorf("cannot decode timestamp file: %v", err)
	}
	if dec.More() {
		return time.Time{}, fmt.Errorf("spurious extra data in timestamp file")
	}
	return d.Timestamp, nil
}

func maybePresentWarnings(count int, timestamp time.Time) {
	if count == 0 {
		return
	}

	if last, _ := lastWarningTimestamp(); !timestamp.After(last) {
		return
	}

	fmt.Fprintf(Stderr,
		i18n.NG("WARNING: There is %d new warning. See 'snap warnings'.\n",
			"WARNING: There are %d new warnings. See 'snap warnings'.\n",
			count),
		count)
}
