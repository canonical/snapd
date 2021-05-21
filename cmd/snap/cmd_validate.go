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
	"fmt"
	"strconv"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdValidate struct {
	clientMixin
	Monitor bool `long:"monitor"`
	// XXX: enforce mode is not supported yet
	Enforce    bool `long:"enforce" hidden:"yes"`
	Forget     bool `long:"forget"`
	Positional struct {
		ValidationSet string `positional-arg-name:"<validation-set>"`
	} `positional-args:"yes"`
	colorMixin
}

var shortValidateHelp = i18n.G("List or apply validation sets")
var longValidateHelp = i18n.G(`
The validate command lists or applies validations sets
`)

func init() {
	cmd := addCommand("validate", shortValidateHelp, longValidateHelp, func() flags.Commander { return &cmdValidate{} }, colorDescs.also(map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"monitor": i18n.G("Monitor the given validations set"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"enforce": i18n.G("Enforce the given validation set"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"forget": i18n.G("Forget the given validation set"),
	}), []argDesc{{
		// TRANSLATORS: This needs to begin with < and end with >
		name: i18n.G("<validation-set>"),
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("Validation set with an optional pinned sequence point, i.e. account-id/name[=seq]"),
	}})
	// XXX: remove once api has landed
	cmd.hidden = true
}

func splitValidationSetArg(arg string) (account, name string, seq int, err error) {
	parts := strings.Split(arg, "=")
	if len(parts) > 2 {
		return "", "", 0, fmt.Errorf("cannot parse validation set, expected account/name=seq")
	}
	if len(parts) == 2 {
		seq, err = strconv.Atoi(parts[1])
		if err != nil {
			return "", "", 0, err
		}
	}

	parts = strings.Split(parts[0], "/")
	if len(parts) != 2 {
		return "", "", 0, fmt.Errorf("expected a single account/name")
	}

	account = parts[0]
	name = parts[1]
	if !asserts.IsValidAccountID(account) {
		return "", "", 0, fmt.Errorf("invalid account ID %q", account)
	}
	if !asserts.IsValidValidationSetName(name) {
		return "", "", 0, fmt.Errorf("invalid validation set name %q", name)
	}

	return account, name, seq, nil
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
	var err error
	if cmd.Positional.ValidationSet != "" {
		accountID, name, seq, err = splitValidationSetArg(cmd.Positional.ValidationSet)
		if err != nil {
			return fmt.Errorf("cannot parse validation set %q: %v", cmd.Positional.ValidationSet, err)
		}
	}

	if action != "" {
		// forget
		if cmd.Forget {
			return cmd.client.ForgetValidationSet(accountID, name, seq)
		}
		// apply
		opts := &client.ValidateApplyOptions{
			Mode:     action,
			Sequence: seq,
		}
		return cmd.client.ApplyValidationSet(accountID, name, opts)
	}

	// no validation set argument, print list with extended info
	if cmd.Positional.ValidationSet == "" {
		vsets, err := cmd.client.ListValidationsSets()
		if err != nil {
			return err
		}
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
		vset, err := cmd.client.ValidationSet(accountID, name, seq)
		if err != nil {
			return err
		}
		fmt.Fprintf(Stdout, fmtValid(vset))
		// XXX: exit status 1 if invalid?
	}

	return nil
}
