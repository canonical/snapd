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
	"io"
	"text/tabwriter"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snap"
)

type cmdAlias struct {
	waitMixin
	Positionals struct {
		SnapApp appName `required:"yes"`
		Alias   string  `required:"yes"`
	} `positional-args:"true"`
}

// TODO: implement a completer for snapApp

var (
	shortAliasHelp = i18n.G("Set up a manual alias")
	longAliasHelp  = i18n.G(`
The alias command aliases the given snap application to the given alias.

Once this manual alias is setup the respective application command can be
invoked just using the alias.
`)
)

func init() {
	addCommand("alias", shortAliasHelp, longAliasHelp, func() flags.Commander {
		return &cmdAlias{}
	}, waitDescs, []argDesc{
		{name: "<snap.app>"},
		// TRANSLATORS: This needs to begin with < and end with >
		{name: i18n.G("<alias>")},
	})
}

func (x *cmdAlias) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	snapName, appName := snap.SplitSnapApp(string(x.Positionals.SnapApp))
	alias := x.Positionals.Alias

	id := mylog.Check2(x.client.Alias(snapName, appName, alias))

	chg := mylog.Check2(x.wait(id))

	return showAliasChanges(chg)
}

type changedAlias struct {
	Snap  string `json:"snap"`
	App   string `json:"app"`
	Alias string `json:"alias"`
}

func showAliasChanges(chg *client.Change) error {
	var added, removed []*changedAlias
	if mylog.Check(chg.Get("aliases-added", &added)); err != nil && err != client.ErrNoData {
		return err
	}
	if mylog.Check(chg.Get("aliases-removed", &removed)); err != nil && err != client.ErrNoData {
		return err
	}
	w := tabwriter.NewWriter(Stdout, 2, 2, 1, ' ', 0)
	if len(added) != 0 {
		// TRANSLATORS: this is used to introduce a list of aliases that were added
		printChangedAliases(w, i18n.G("Added"), added)
	}
	if len(removed) != 0 {
		// TRANSLATORS: this is used to introduce a list of aliases that were removed
		printChangedAliases(w, i18n.G("Removed"), removed)
	}
	w.Flush()
	return nil
}

func printChangedAliases(w io.Writer, label string, changed []*changedAlias) {
	fmt.Fprintf(w, "%s:\n", label)
	for _, a := range changed {
		// TRANSLATORS: the first %s is a snap command (e.g. "hello-world.echo"), the second is the alias
		fmt.Fprintf(w, i18n.G("\t- %s as %s\n"), snap.JoinSnapApp(a.Snap, a.App), a.Alias)
	}
}
