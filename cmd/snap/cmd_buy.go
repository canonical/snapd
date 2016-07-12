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
	Currency  string `long:"currency" description:"ISO 4217 code for currency (https://en.wikipedia.org/wiki/ISO_4217)"`
	Channel   string `long:"channel" description:"Use this channel instead of stable"`
	BackendID string `long:"backend-id" description:"e.g. \"credit_card\", \"rest_paypal\""`
	MethodID  int    `long:"method-id" description:"numeric identifier for a specific credit card or Paypal account"`

	Positional struct {
		SnapName string `positional-arg-name:"<snap-name>"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addCommand("buy", shortBuyHelp, longBuyHelp, func() flags.Commander {
		return &cmdBuy{}
	})
}

func (x *cmdBuy) Execute([]string) error {
	return buySnap(&store.BuyOptions{
		SnapName:  x.Positional.SnapName,
		Currency:  x.Currency,
		BackendID: x.BackendID,
		MethodID:  x.MethodID,
		Channel:   x.Channel,
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
		return fmt.Errorf(i18n.G("cannot buy snap %q: it cannot be found"), opts.SnapName)
	}

	if len(snaps) > 1 {
		return fmt.Errorf(i18n.G("cannot buy snap %q: muliple results found"), opts.SnapName)
	}

	snap := snaps[0]

	opts.SnapID = snap.ID

	if opts.Channel == "" {
		opts.Channel = snap.Channel
	}

	if opts.Currency == "" {
		opts.Currency = resultInfo.SuggestedCurrency
	}

	opts.Price, opts.Currency, err = getPrice(snap.Prices, opts.Currency)
	if err != nil {
		return fmt.Errorf(i18n.G("cannot buy snap %q: it is free"), opts.SnapName)
	}

	if snap.Status != "priced" {
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
		return fmt.Errorf(i18n.G("buying snap %q cancelled by user"), opts.SnapName)
	}

	result, err := cli.Buy(opts)
	if err != nil {
		return err
	}

	// TODO Handle pay backends that require user interaction
	fmt.Fprintf(Stdout, "Buy state: %s\n", result.State)

	return nil
}
