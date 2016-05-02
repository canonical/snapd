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
	"text/tabwriter"

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"

	"github.com/jessevdk/go-flags"
)

var shortFindHelp = i18n.G("Finds packages to install")
var longFindHelp = i18n.G(`
The find command queries the store for available packages.
`)

func getPrice(prices map[string]float64, currency string) string {
	// If there are no prices, then the snap is free
	if len(prices) == 0 {
		return "-"
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

func hasPrices(snaps []*client.Snap) bool {
	for _, snap := range snaps {
		if len(snap.Prices) > 0 {
			return true
		}
	}
	return false
}

func printWithPrices(w *tabwriter.Writer, snaps []*client.Snap, suggestedCurrency string) {
	fmt.Fprintln(w, i18n.G("Name\tVersion\tPrice\tSummary"))

	for _, snap := range snaps {
		price := getPrice(snap.Prices, suggestedCurrency)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", snap.Name, snap.Version, price, snap.Summary)

	}
}

func printNoPrices(w *tabwriter.Writer, snaps []*client.Snap) {
	fmt.Fprintln(w, i18n.G("Name\tVersion\tSummary"))

	for _, snap := range snaps {
		fmt.Fprintf(w, "%s\t%s\t%s\n", snap.Name, snap.Version, snap.Summary)
	}
}

func (x *cmdFind) Execute([]string) error {
	cli := Client()
	filter := client.SnapFilter{
		Query:   x.Positional.Query,
		Sources: []string{"store"},
	}
	snaps, resInfo, err := cli.FilterSnaps(filter)
	if err != nil {
		return err
	}

	if len(snaps) == 0 {
		if filter.Query == "" {
			return fmt.Errorf("no snaps found")
		}

		return fmt.Errorf("no snaps found for %q", filter.Query)
	}

	sort.Sort(snapsByName(snaps))

	w := tabWriter()
	defer w.Flush()

	if hasPrices(snaps) {
		printWithPrices(w, snaps, resInfo.SuggestedCurrency)
	} else {
		printNoPrices(w, snaps)
	}

	return nil
}
