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
	"regexp"
	"strconv"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdValidate struct {
	clientMixin
	Monitor    bool `long:"monitor"`
	Enforce    bool `long:"enforce"`
	Forget     bool `long:"forget"`
	Positional struct {
		ValidationSet string `positional-arg-name:"<validation-set>"`
	} `positional-args:"yes"`
	colorMixin
}

var shortValidateHelp = i18n.G("List or set snap validations")
var longValidateHelp = i18n.G(`
The validate command lists validation sets or sets validations
`)

func init() {
	addCommand("validate", shortValidateHelp, longValidateHelp, func() flags.Commander { return &cmdValidate{} }, colorDescs.also(map[string]string{
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
		desc: i18n.G("Validation set with an optional pinned sequence point, i.e. account/name[=seq]"),
	}})
}

// this is reused for both account and set name of "account/name" argument
var validName = regexp.MustCompile("^[a-z][0-9a-z]+$")

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
	if !validName.MatchString(account) {
		return "", "", 0, fmt.Errorf("invalid account name %q", account)
	}
	if !validName.MatchString(name) {
		return "", "", 0, fmt.Errorf("invalid name %q", name)
	}

	return account, name, seq, nil
}

func fmtValid(res *client.ValidationSetResult) string {
	if res.Valid {
		return fmt.Sprintf("valid")
	}
	return fmt.Sprintf("invalid")
}

func (cmd *cmdValidate) Execute(args []string) error {
	// check that only one flag is used at a time
	var validateFlag string
	for _, flag := range []struct {
		name string
		set  bool
	}{
		{"monitor", cmd.Monitor},
		{"enforce", cmd.Enforce},
		{"forget", cmd.Forget},
	} {
		if flag.set {
			if validateFlag != "" {
				return fmt.Errorf("cannot use --%s and --%s together", validateFlag, flag.name)
			}
			validateFlag = flag.name
		}
	}

	if cmd.Positional.ValidationSet == "" && validateFlag != "" {
		return fmt.Errorf("missing validation set argument")
	}

	var account, name string
	var seq int
	var err error
	if cmd.Positional.ValidationSet != "" {
		account, name, seq, err = splitValidationSetArg(cmd.Positional.ValidationSet)
		if err != nil {
			return fmt.Errorf("cannot parse validation set %q: %v", cmd.Positional.ValidationSet, err)
		}
	}

	if validateFlag != "" {
		// apply
		opts := &client.ValidateApplyOptions{
			Flag:  validateFlag,
			PinAt: seq,
		}
		return cmd.client.ApplyValidationSet(account, name, opts)
	}

	// validate
	vsets, err := cmd.client.QueryValidationSet(account, name, seq)
	if err != nil {
		return err
	}
	// no validation set argument, print list with extended info
	if cmd.Positional.ValidationSet == "" {
		if len(vsets) == 0 {
			return nil
		}

		esc := cmd.getEscapes()
		w := tabWriter()

		// TRANSLATORS: the %s is to insert a filler escape sequence (please keep it flush to the column header, with no extra spaces)
		fmt.Fprintf(w, i18n.G("Validation\tMode\tSeq\tCurrent\t%s\tNotes\n"), fillerPublisher(esc))
		for _, res := range vsets {
			// doing it this way because otherwise it's a sea of %s\t%s\t%s
			line := []string{
				res.ValidationSet,
				res.Mode,
				fmt.Sprintf("%d", res.Seq),
				fmtValid(res),
				strings.Join(res.Notes, ","),
			}
			fmt.Fprintln(w, strings.Join(line, "\t"))
		}
		w.Flush()
		fmt.Fprintln(Stdout)

	} else {
		// specific validation set was queried, so we expect exactly one result.
		fmt.Fprintf(Stdout, fmtValid(vsets[0]))
		// XXX: exit status 1 if invalid?
	}

	return nil
}
