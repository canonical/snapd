// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
)

var shortFindHelp = i18n.G("Finds packages to install")
var longFindHelp = i18n.G(`
The find command queries the store for available packages in the stable channel.
`)

func getPrice(prices map[string]float64, currency string) (float64, string, error) {
	// If there are no prices, then the snap is free
	if len(prices) == 0 {
		// TRANSLATORS: free as in gratis
		return 0, "", errors.New(i18n.G("snap is free"))
	}

	// Look up the price by currency code
	val, ok := prices[currency]

	// Fall back to dollars
	if !ok {
		currency = "USD"
		val, ok = prices["USD"]
	}

	// If there aren't even dollars, grab the first currency,
	// ordered alphabetically by currency code
	if !ok {
		currency = "ZZZ"
		for c, v := range prices {
			if c < currency {
				currency, val = c, v
			}
		}
	}

	return val, currency, nil
}

type SectionName string

func (s SectionName) Complete(match string) []flags.Completion {
	if ret, err := completeFromSortedFile(dirs.SnapSectionsFile, match); err == nil {
		return ret
	}

	cli := Client()
	sections, err := cli.Sections()
	if err != nil {
		return nil
	}
	ret := make([]flags.Completion, 0, len(sections))
	for _, s := range sections {
		if strings.HasPrefix(s, match) {
			ret = append(ret, flags.Completion{Item: s})
		}
	}
	return ret
}

type cmdFind struct {
	Private    bool        `long:"private"`
	Section    SectionName `long:"section"`
	Positional struct {
		Query string
	} `positional-args:"yes"`
}

func init() {
	addCommand("find", shortFindHelp, longFindHelp, func() flags.Commander {
		return &cmdFind{}
	}, map[string]string{
		"private": i18n.G("Search private snaps"),
		"section": i18n.G("Restrict the search to a given section"),
	}, []argDesc{{
		// TRANSLATORS: This needs to be wrapped in <>s.
		name: i18n.G("<query>"),
	}}).alias = "search"
}

func (x *cmdFind) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	// LP: 1740605
	if strings.TrimSpace(x.Positional.Query) == "" {
		x.Positional.Query = ""
	}

	// magic! `snap find` returns the featured snaps
	showFeatured := (x.Positional.Query == "" && x.Section == "")
	if showFeatured {
		x.Section = "featured"
	}

	cli := Client()
	opts := &client.FindOptions{
		Private: x.Private,
		Section: string(x.Section),
		Query:   x.Positional.Query,
	}
	snaps, resInfo, err := cli.Find(opts)
	if e, ok := err.(*client.Error); ok && e.Kind == client.ErrorKindNetworkTimeout {
		logger.Debugf("cannot list snaps: %v", e)
		return fmt.Errorf("unable to contact snap store")
	}
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		// TRANSLATORS: the %q is the (quoted) query the user entered
		fmt.Fprintf(Stderr, i18n.G("No matching snaps for %q\n"), opts.Query)
		return nil
	}

	// show featured header *after* we checked for errors from the find
	if showFeatured {
		fmt.Fprintf(Stdout, i18n.G("No search term specified. Here are some interesting snaps:\n\n"))
	}

	w := tabWriter()
	fmt.Fprintln(w, i18n.G("Name\tVersion\tDeveloper\tNotes\tSummary"))
	for _, snap := range snaps {
		// TODO: get snap.Publisher, so we can only show snap.Developer if it's different
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", snap.Name, snap.Version, snap.Developer, NotesFromRemote(snap, resInfo), snap.Summary)
	}
	w.Flush()

	if showFeatured {
		fmt.Fprintf(Stdout, i18n.G("\nProvide a search term for more specific results.\n"))
	}
	return nil
}
