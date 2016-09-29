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
	Currency string `long:"currency"`

	Positional struct {
		SnapName string
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addCommand("buy", shortBuyHelp, longBuyHelp, func() flags.Commander {
		return &cmdBuy{}
	}, map[string]string{
		"currency": i18n.G("ISO 4217 code for currency (https://en.wikipedia.org/wiki/ISO_4217)"),
	}, []argDesc{{
		name: "<snap>",
		desc: i18n.G("Snap name"),
	}})
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

	if !cli.LoggedIn() {
		return fmt.Errorf(i18n.G("You need to be logged in to purchase software. Please run 'snap login' and try again."))
	}

	if strings.ContainsAny(opts.SnapName, ":*") {
		return fmt.Errorf(i18n.G("cannot buy snap %q: invalid characters in name"), opts.SnapName)
	}

	snap, resultInfo, err := cli.FindOne(opts.SnapName)
	if err != nil {
		return err
	}

	opts.SnapID = snap.ID
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

	err = cli.ReadyToBuy()
	if err != nil {
		if e, ok := err.(*client.Error); ok {
			switch e.Kind {
			case client.ErrorKindNoPaymentMethods:
				return fmt.Errorf(i18n.G(`You do not have a payment method associated with your account, visit https://my.ubuntu.com/payment/edit to add one.
Once completed, return here and run 'snap buy %s' again.`), snap.Name)
			case client.ErrorKindTermsNotAccepted:
				return fmt.Errorf(i18n.G(`Please visit https://my.ubuntu.com/terms to agree to the latest terms and conditions.
Once completed, return here and run 'snap buy %s' again.`), snap.Name)
			}
		}
		return err
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

	result, err := cli.Buy(opts)
	if err != nil {
		return err
	}

	if result.State == "InProgress" {
		// TODO Support interactive purchases on the CLI
		return fmt.Errorf(i18n.G("cannot buy snap %q: the command line tools do not support interactive purchases"), snap.Name)
	}

	// TRANSLATORS: %s is a snap name
	fmt.Fprintf(Stdout, i18n.G("Thanks for purchasing %s. You may now install it on any of your devices with 'snap install %s'.\n"), opts.SnapName, opts.SnapName)

	return nil
}
