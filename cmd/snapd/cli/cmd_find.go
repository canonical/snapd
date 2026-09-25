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

package cli

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
The find command queries the store for available packages.

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

const (
	// sectionShowAllValue is used when the user specified --section
	// without any argument.
	sectionShowAllValue = "show-all-sections-please"
	// sectionDefaultValue is used when "--section" was not specified on
	// the commandline at all.
	sectionDefaultValue = "no-section-specified"
)

type SectionName string

// UnmarshalFlag accumulates repeated --section flags into a comma-separated
// list instead of overwriting, since the store already matches against any
// of them.
func (s *SectionName) UnmarshalFlag(value string) error {
	switch string(*s) {
	case "", sectionDefaultValue:
		*s = SectionName(value)
	case sectionShowAllValue:
		// a bare --section (show all sections) was already given; an
		// explicit value after that doesn't combine sensibly with "show
		// all", so let the explicit value take over instead of joining
		*s = SectionName(value)
	default:
		if value == sectionShowAllValue {
			// bare --section after one or more explicit values: keep
			// filtering rather than switching to "show all sections"
			return nil
		}
		*s += "," + SectionName(value)
	}
	return nil
}

func (s SectionName) Complete(match string) []flags.Completion {
	if ret, err := completeFromSortedFile(dirs.SnapSectionsFile, match); err == nil {
		return ret
	}

	cli := mkClient()
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

func unknownSections(requested, known []string) []string {
	var unknown []string
	for _, r := range requested {
		if !strutil.ListContains(known, r) {
			unknown = append(unknown, r)
		}
	}
	return unknown
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

func getSections(cli *client.Client) (sections []string, err error) {
	// try loading from cached sections file
	sections, err = cachedSections()
	if err != nil {
		return nil, err
	}
	if sections != nil {
		return sections, nil
	}
	// fallback to listing from the daemon
	return cli.Sections()
}

func showSections(cli *client.Client) error {
	sections, err := getSections(cli)
	if err != nil {
		return err
	}
	sort.Strings(sections)

	fmt.Fprint(Stdout, i18n.G("No section specified. Available sections:\n"))
	for _, sec := range sections {
		fmt.Fprintf(Stdout, " * %s\n", sec)
	}
	fmt.Fprint(Stdout, i18n.G("Please try 'snap find --section=<selected section>'\n"))
	return nil
}

type cmdFind struct {
	clientMixin
	Private    bool        `long:"private"`
	Narrow     bool        `long:"narrow"`
	Section    SectionName `long:"section" optional:"true" optional-value:"show-all-sections-please" default:"no-section-specified" default-mask:"-"`
	Positional struct {
		Query []string
	} `positional-args:"yes"`
	colorMixin
}

func init() {
	addCommand("find", shortFindHelp, longFindHelp, func() flags.Commander {
		return &cmdFind{}
	}, colorDescs.also(map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"private": i18n.G("Search private snaps."),
		// TRANSLATORS: This should not start with a lowercase letter.
		"narrow": i18n.G("Only search for snaps in “stable”."),
		// TRANSLATORS: This should not start with a lowercase letter.
		"section": i18n.G("Restrict the search to a given section."),
	}), []argDesc{{
		// TRANSLATORS: This needs to begin with < and end with >
		name: i18n.G("<query>"),
	}}).alias = "search"

}

func (x *cmdFind) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	// LP: 1740605
	query := strings.Join(x.Positional.Query, " ")
	if strings.TrimSpace(query) == "" {
		query = ""
	}

	switch x.Section {
	case sectionShowAllValue:
		return showSections(x.client)
	case sectionDefaultValue:
		x.Section = ""
	}

	// magic! `snap find` returns the featured snaps
	showFeatured := (query == "" && x.Section == "")
	if showFeatured {
		x.Section = "featured"
	}

	if x.Section != "" && x.Section != "featured" {
		requested := strutil.CommaSeparatedList(string(x.Section))
		sections, err := cachedSections()
		if err != nil {
			return err
		}
		unknown := unknownSections(requested, sections)
		if len(unknown) > 0 {
			// try the store just in case they were added in the last 24 hours
			sections, err = x.client.Sections()
			if err != nil {
				return err
			}
			unknown = unknownSections(requested, sections)
			if len(unknown) > 0 {
				// TRANSLATORS: %s is a comma-separated list of one or more (quoted) section names the user entered
				return fmt.Errorf(i18n.G("No matching section(s) %s, use --section to list existing sections"), strutil.Quoted(unknown))
			}
		}
	}

	opts := &client.FindOptions{
		Query:   query,
		Section: string(x.Section),
		Private: x.Private,
	}

	if !x.Narrow {
		opts.Scope = "wide"
	}

	snaps, resInfo, err := x.client.Find(opts)
	if e, ok := err.(*client.Error); ok && (e.Kind == client.ErrorKindNetworkTimeout || e.Kind == client.ErrorKindDNSFailure) {
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
		fmt.Fprint(Stdout, i18n.G("No search term specified. Here are some interesting snaps:\n\n"))
	}

	esc := x.getEscapes()
	w := tabWriter()
	// TRANSLATORS: the %s is to insert a filler escape sequence (please keep it flush to the column header, with no extra spaces)
	fmt.Fprintf(w, i18n.G("Name\tVersion\tPublisher%s\tNotes\tSummary\n"), fillerPublisher(esc))
	for _, snap := range snaps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", snap.Name, snap.Version, shortPublisher(esc, snap.Publisher), NotesFromRemote(snap, resInfo), snap.Summary)
	}
	w.Flush()
	if showFeatured {
		fmt.Fprint(Stdout, i18n.G("\nProvide a search term for more specific results.\n"))
	}
	return nil
}
