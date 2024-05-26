// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/strutil"
)

type cmdValidate struct {
	Monitor    bool `long:"monitor"`
	Enforce    bool `long:"enforce"`
	Forget     bool `long:"forget"`
	Refresh    bool `long:"refresh"`
	Positional struct {
		ValidationSet string `positional-arg-name:"<validation-set>"`
	} `positional-args:"yes"`
	colorMixin
	waitMixin
}

var (
	shortValidateHelp = i18n.G("List or apply validation sets")
	longValidateHelp  = i18n.G(`
The validate command lists or applies validation sets that state which snaps
are required or permitted to be installed together, optionally constrained to
fixed revisions.

A validation set can either be in monitoring mode, in which case its constraints
aren't enforced, or in enforcing mode, in which case snapd will not allow
operations which would result in snaps breaking the validation set's constraints.
`)
)

func init() {
	addCommand("validate", shortValidateHelp, longValidateHelp, func() flags.Commander { return &cmdValidate{} }, waitDescs.also(colorDescs.also(map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"monitor": i18n.G("Monitor the given validations set"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"enforce": i18n.G("Enforce the given validation set"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"forget": i18n.G("Forget the given validation set"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"refresh": i18n.G("Refresh or install snaps to satisfy enforced validation sets"),
	})), []argDesc{{
		// TRANSLATORS: This needs to begin with < and end with >
		name: i18n.G("<validation-set>"),
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("Validation set with an optional pinned sequence point, i.e. account-id/name[=seq]"),
	}})
}

func fmtValid(res *client.ValidationSetResult) string {
	if res.Valid {
		return "valid"
	}
	return "invalid"
}

func fmtValidationSet(res *client.ValidationSetResult) string {
	if res.PinnedAt == 0 {
		return fmt.Sprintf("%s/%s", res.AccountID, res.Name)
	}
	return fmt.Sprintf("%s/%s=%d", res.AccountID, res.Name, res.PinnedAt)
}

func (cmd *cmdValidate) Execute(args []string) error {
	// check that only one action is used at a time
	var action string
	for _, a := range []struct {
		name string
		set  bool
	}{
		{"monitor", cmd.Monitor},
		{"enforce", cmd.Enforce},
		{"forget", cmd.Forget},
	} {
		if a.set {
			if action != "" {
				return fmt.Errorf("cannot use --%s and --%s together", action, a.name)
			}
			action = a.name
		}
	}

	if cmd.Positional.ValidationSet == "" && action != "" {
		return fmt.Errorf("missing validation set argument")
	}

	var accountID, name string
	var seq int

	if cmd.Positional.ValidationSet != "" {
		accountID, name, seq = mylog.Check4(snapasserts.ParseValidationSet(cmd.Positional.ValidationSet))
	}

	if action != "" {
		if cmd.Refresh && action != "enforce" {
			return fmt.Errorf("--refresh can only be used together with --enforce")
		}

		if cmd.Refresh {
			changeID := mylog.Check2(cmd.client.RefreshMany(nil, &client.SnapOptions{
				ValidationSets: []string{cmd.Positional.ValidationSet},
			}))

			chg := mylog.Check2(cmd.wait(changeID))

			var names []string
			if mylog.Check(chg.Get("snap-names", &names)); err != nil && !errors.Is(err, client.ErrNoData) {
				return err
			}

			if len(names) != 0 {
				fmt.Fprintf(Stdout, i18n.G("Refreshed/installed snaps %s to enforce validation set %q\n"), strutil.Quoted(names), cmd.Positional.ValidationSet)
			} else {
				fmt.Fprintf(Stdout, i18n.G("Enforced validation set %q\n"), cmd.Positional.ValidationSet)
			}

			return nil
		}

		// forget
		if cmd.Forget {
			return cmd.client.ForgetValidationSet(accountID, name, seq)
		}
		// apply
		opts := &client.ValidateApplyOptions{
			Mode:     action,
			Sequence: seq,
		}
		res := mylog.Check2(cmd.client.ApplyValidationSet(accountID, name, opts))

		// only print valid/invalid status for monitor mode; enforce fails with an error if invalid
		// and otherwise has no output.
		if action == "monitor" {
			fmt.Fprintln(Stdout, fmtValid(res))
		}
		return nil
	}

	// no validation set argument, print list with extended info
	if cmd.Positional.ValidationSet == "" {
		vsets := mylog.Check2(cmd.client.ListValidationsSets())

		if len(vsets) == 0 {
			fmt.Fprintln(Stderr, i18n.G("No validations are available"))
			return nil
		}

		esc := cmd.getEscapes()
		w := tabWriter()

		// TRANSLATORS: the %s is to insert a filler escape sequence (please keep it flush to the column header, with no extra spaces)
		fmt.Fprintf(w, i18n.G("Validation\tMode\tSeq\tCurrent\t%s\tNotes\n"), fillerPublisher(esc))
		for _, res := range vsets {
			// TODO: fill notes when've clarity about them
			var notes string
			// doing it this way because otherwise it's a sea of %s\t%s\t%s
			line := []string{
				fmtValidationSet(res),
				res.Mode,
				fmt.Sprintf("%d", res.Sequence),
				fmtValid(res),
				notes,
			}
			fmt.Fprintln(w, strings.Join(line, "\t"))
		}
		w.Flush()
	} else {
		vset := mylog.Check2(cmd.client.ValidationSet(accountID, name, seq))

		fmt.Fprintf(Stdout, fmtValid(vset))
		// XXX: exit status 1 if invalid?
	}

	return nil
}
