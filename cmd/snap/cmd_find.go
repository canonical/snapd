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
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/strutil"
)

var shortFindHelp = i18n.G("Find packages to install")
var longFindHelp = i18n.G(`
The find command queries the store for available packages in the stable channel.

With the --private flag, which requires the user to be logged-in to the store
(see 'snap help login'), it instead searches for private snaps that the user
has developer access to, either directly or through the store's collaboration
feature.

A green check mark (given color and unicode support) after a publisher name
indicates that the publisher has been verified.
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

func cachedSections() (sections []string, err error) {
	cachedSections, err := os.Open(dirs.SnapSectionsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer cachedSections.Close()

	r := bufio.NewScanner(cachedSections)
	for r.Scan() {
		sections = append(sections, r.Text())
	}
	if r.Err() != nil {
		return nil, r.Err()
	}

	return sections, nil
}

func getSections() (sections []string, err error) {
	// try loading from cached sections file
	sections, err = cachedSections()
	if err != nil {
		return nil, err
	}
	if sections != nil {
		return sections, nil
	}
	// fallback to listing from the daemon
	cli := Client()
	return cli.Sections()
}

func showSections() error {
	sections, err := getSections()
	if err != nil {
		return err
	}
	sort.Strings(sections)

	fmt.Fprintf(Stdout, i18n.G("No section specified. Available sections:\n"))
	for _, sec := range sections {
		fmt.Fprintf(Stdout, " * %s\n", sec)
	}
	fmt.Fprintf(Stdout, i18n.G("Please try 'snap find --section=<selected section>'\n"))
	return nil
}

type cmdFind struct {
	Private    bool        `long:"private"`
	Narrow     bool        `long:"narrow"`
	Section    SectionName `long:"section" optional:"true" optional-value:"show-all-sections-please" default:"no-section-specified"`
	Positional struct {
		Query string
	} `positional-args:"yes"`
	colorMixin
}

func init() {
	addCommand("find", shortFindHelp, longFindHelp, func() flags.Commander {
		return &cmdFind{}
	}, colorDescs.also(map[string]string{
		"private": i18n.G("Search private snaps"),
		"narrow":  i18n.G("Only search for snaps in “stable”"),
		"section": i18n.G("Restrict the search to a given section"),
	}), []argDesc{{
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

	// section will be:
	// - "show-all-sections-please" if the user specified --section
	//   without any argument
	// - "no-section-specified" if "--section" was not specified on
	//   the commandline at all
	switch x.Section {
	case "show-all-sections-please":
		return showSections()
	case "no-section-specified":
		x.Section = ""
	}

	// magic! `snap find` returns the featured snaps
	showFeatured := (x.Positional.Query == "" && x.Section == "")
	if showFeatured {
		x.Section = "featured"
	}

	cli := Client()

	if x.Section != "" && x.Section != "featured" {
		sections, err := cachedSections()
		if err != nil {
			return err
		}
		if !strutil.ListContains(sections, string(x.Section)) {
			// try the store just in case it was added in the last 24 hours
			sections, err = cli.Sections()
			if err != nil {
				return err
			}
			if !strutil.ListContains(sections, string(x.Section)) {
				// TRANSLATORS: the %q is the (quoted) name of the section the user entered
				return fmt.Errorf(i18n.G("No matching section %q, use --section to list existing sections"), x.Section)
			}
		}
	}

	opts := &client.FindOptions{
		Private: x.Private,
		Section: string(x.Section),
		Query:   x.Positional.Query,
	}

	if !x.Narrow {
		opts.Scope = "wide"
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
		if x.Section == "" {
			// TRANSLATORS: the %q is the (quoted) query the user entered
			fmt.Fprintf(Stderr, i18n.G("No matching snaps for %q\n"), opts.Query)
		} else {
			// TRANSLATORS: the first %q is the (quoted) query, the
			// second %q is the (quoted) name of the section the
			// user entered
			fmt.Fprintf(Stderr, i18n.G("No matching snaps for %q in section %q\n"), opts.Query, x.Section)
		}
		return nil
	}

	// show featured header *after* we checked for errors from the find
	if showFeatured {
		fmt.Fprintf(Stdout, i18n.G("No search term specified. Here are some interesting snaps:\n\n"))
	}

	esc := x.getEscapes()
	w := tabWriter()
	// TRANSLATORS: the %s is to insert a filler escape sequence (careful with the spacing please)
	fmt.Fprintf(w, i18n.G("Name\tVersion\tPublisher%s\tNotes\tSummary\n"), esc.filler())
	for _, snap := range snaps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", snap.Name, snap.Version, esc.shortPublisher(snap.Publisher), NotesFromRemote(snap, resInfo), snap.Summary)
	}
	w.Flush()
	if showFeatured {
		fmt.Fprintln(Stdout, i18n.G("\nProvide a search term for more specific results."))
	}
	return nil
}
