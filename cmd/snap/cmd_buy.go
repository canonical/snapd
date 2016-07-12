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
	"bufio"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/store"

	"github.com/jessevdk/go-flags"
)

var shortBuyHelp = i18n.G("Buys a snap")
var longBuyHelp = i18n.G(`
The buy command buys a snap from the store.
`)

var positiveResponse = map[string]bool{
	"":            true,
	i18n.G("y"):   true,
	i18n.G("yes"): true,
}

type cmdBuy struct {
	Currency string `long:"currency" description:"ISO 4217 code for currency (https://en.wikipedia.org/wiki/ISO_4217)"`

	Positional struct {
		SnapName string `positional-arg-name:"<snap-name>"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addCommand("buy", shortBuyHelp, longBuyHelp, func() flags.Commander {
		return &cmdBuy{}
	})
}

func (x *cmdBuy) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return buySnap(&store.BuyOptions{
		SnapName: x.Positional.SnapName,
		Currency: x.Currency,
	})
}

func buySnap(opts *store.BuyOptions) error {
	cli := Client()

	if strings.ContainsAny(opts.SnapName, ":*") {
		return fmt.Errorf(i18n.G("cannot buy snap %q: invalid characters in name"), opts.SnapName)
	}

	snaps, resultInfo, err := cli.Find(&client.FindOptions{
		Query: fmt.Sprintf("name:%s", opts.SnapName),
	})

	if err != nil {
		return err
	}

	if len(snaps) < 1 {
		return fmt.Errorf(i18n.G("cannot find snap %q"), opts.SnapName)
	}

	if len(snaps) > 1 {
		return fmt.Errorf(i18n.G("cannot buy snap %q: muliple results found"), opts.SnapName)
	}

	snap := snaps[0]

	opts.SnapID = snap.ID
	opts.Channel = snap.Channel
	if opts.Currency == "" {
		opts.Currency = resultInfo.SuggestedCurrency
	}

	opts.Price, opts.Currency, err = getPrice(snap.Prices, opts.Currency)
	if err != nil {
		return fmt.Errorf(i18n.G("cannot buy snap %q: %v"), opts.SnapName, err)
	}

	if snap.Status == "available" {
		return fmt.Errorf(i18n.G("cannot buy snap %q: it has already been bought"), opts.SnapName)
	}

	reader := bufio.NewReader(nil)
	reader.Reset(Stdin)

	fmt.Fprintf(Stdout, i18n.G("Do you want to buy %q from %q for %s? (Y/n): "), snap.Name,
		snap.Developer, formatPrice(opts.Price, opts.Currency))

	response, _, err := reader.ReadLine()
	if err != nil {
		return err
	}

	if !positiveResponse[strings.ToLower(string(response))] {
		return fmt.Errorf(i18n.G("aborting"))
	}

	// TODO Handle pay backends that require user interaction
	_, err = cli.Buy(opts)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, "%s bought\n", opts.SnapName)

	return nil
}
