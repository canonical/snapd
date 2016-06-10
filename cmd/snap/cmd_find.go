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
	"sort"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

var shortFindHelp = i18n.G("Finds packages to install")
var longFindHelp = i18n.G(`
The find command queries the store for available packages.
`)

func getPrice(prices map[string]float64, currency string) string {
	// If there are no prices, then the snap is free
	if len(prices) == 0 {
		return ""
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

	return fmt.Sprintf("%.2f%s", val, currency)
}

type cmdFind struct {
	Positional struct {
		Query string `positional-arg-name:"<query>"`
	} `positional-args:"yes"`
}

func init() {
	addCommand("find", shortFindHelp, longFindHelp, func() flags.Commander {
		return &cmdFind{}
	})
}

func (x *cmdFind) Execute([]string) error {
	return findSnaps(&client.FindOptions{
		Query: x.Positional.Query,
	})
}

func findSnaps(opts *client.FindOptions) error {
	cli := Client()
	snaps, resInfo, err := cli.Find(opts)
	if err != nil {
		return err
	}

	if len(snaps) == 0 {
		return fmt.Errorf("no snaps found for %q", opts.Query)
	}

	sort.Sort(snapsByName(snaps))

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Name\tVersion\tDeveloper\tNotes\tSummary"))

	for _, snap := range snaps {
		notes := &Notes{
			Private:     snap.Private,
			Confinement: snap.Confinement,
			Price:       getPrice(snap.Prices, resInfo.SuggestedCurrency),
		}
		// TODO: get snap.Publisher, so we can only show snap.Developer if it's different
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", snap.Name, snap.Version, snap.Developer, notes, snap.Summary)
	}

	return nil
}
