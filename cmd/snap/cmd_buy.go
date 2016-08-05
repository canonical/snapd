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
	"strconv"
	"strings"
	"text/tabwriter"

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

	paymentInfo, err := cli.PaymentMethods()
	if err != nil {
		return err
	}

	if len(paymentInfo.Methods) == 0 {
		return fmt.Errorf(i18n.G("cannot buy snap %q: no payment methods registered"), opts.SnapName)
	}

	reader := bufio.NewReader(nil)
	reader.Reset(Stdin)

	// Unless the user has enabled automatic payments, we must prompt for a payment method.
	if !paymentInfo.AllowsAutomaticPayment {
		w := tabwriter.NewWriter(Stdout, 0, 3, 2, ' ', 0)

		index := -1

		fmt.Fprintln(w, i18n.G("\tSelection\tDescription"))
		for i, method := range paymentInfo.Methods {
			preferred := ""
			if method.Preferred {
				preferred = "*"
				index = i + 1
			}
			fmt.Fprintf(w, "%s\t%d\t%s\n", preferred, i+1, method.Description)
		}
		w.Flush()

		if index > 0 {
			// If the user has a preferred payment method
			fmt.Fprintf(Stdout, i18n.G("Press <enter> to use your default[*], or type a number to select payment method: "))
		} else {
			fmt.Fprintf(Stdout, i18n.G("Type a number to select payment method: "))
		}

		response, _, err := reader.ReadLine()
		if err != nil {
			return err
		}

		stringResponse := string(response)

		if !(stringResponse == "" && index > 0) {
			// If the user answered and also has no preferred method, we need to parse the response.
			index, err = strconv.Atoi(stringResponse)
			if err != nil {
				return fmt.Errorf(i18n.G("cannot buy snap %q: invalid payment method selection %q"), snap.Name, stringResponse)
			}

			if index <= 0 || index > len(paymentInfo.Methods) {
				return fmt.Errorf(i18n.G("cannot buy snap %q: unknown payment method selection %d"), snap.Name, index)
			}
		}

		// Convert the payment selection to a zero-index
		paymentMethod := paymentInfo.Methods[index-1]
		opts.BackendID = paymentMethod.BackendID
		opts.MethodID = paymentMethod.ID
	}

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
