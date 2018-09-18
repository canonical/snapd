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
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/store"

	"github.com/jessevdk/go-flags"
)

var shortBuyHelp = i18n.G("Buy a snap")
var longBuyHelp = i18n.G(`
The buy command buys a snap from the store.
`)

type cmdBuy struct {
	Positional struct {
		SnapName remoteSnapName
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addCommand("buy", shortBuyHelp, longBuyHelp, func() flags.Commander {
		return &cmdBuy{}
	}, map[string]string{}, []argDesc{{
		name: "<snap>",
		// TRANSLATORS: This should probably not start with a lowercase letter.
		desc: i18n.G("Snap name"),
	}})
}

func (x *cmdBuy) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return buySnap(string(x.Positional.SnapName))
}

func buySnap(snapName string) error {
	cli := Client()

	user := cli.LoggedInUser()
	if user == nil {
		return fmt.Errorf(i18n.G("You need to be logged in to purchase software. Please run 'snap login' and try again."))
	}

	if strings.ContainsAny(snapName, ":*") {
		return fmt.Errorf(i18n.G("cannot buy snap: invalid characters in name"))
	}

	snap, resultInfo, err := cli.FindOne(snapName)
	if err != nil {
		return err
	}

	opts := &store.BuyOptions{
		SnapID:   snap.ID,
		Currency: resultInfo.SuggestedCurrency,
	}

	opts.Price, opts.Currency, err = getPrice(snap.Prices, opts.Currency)
	if err != nil {
		return fmt.Errorf(i18n.G("cannot buy snap: %v"), err)
	}

	if snap.Status == "available" {
		return fmt.Errorf(i18n.G("cannot buy snap: it has already been bought"))
	}

	err = cli.ReadyToBuy()
	if err != nil {
		if e, ok := err.(*client.Error); ok {
			switch e.Kind {
			case client.ErrorKindNoPaymentMethods:
				return fmt.Errorf(i18n.G(`You need to have a payment method associated with your account in order to buy a snap, please visit https://my.ubuntu.com/payment/edit to add one.

Once you’ve added your payment details, you just need to run 'snap buy %s' again.`), snap.Name)
			case client.ErrorKindTermsNotAccepted:
				return fmt.Errorf(i18n.G(`In order to buy %q, you need to agree to the latest terms and conditions. Please visit https://my.ubuntu.com/payment/edit to do this.

Once completed, return here and run 'snap buy %s' again.`), snap.Name, snap.Name)
			}
		}
		return err
	}

	// TRANSLATORS: %q, %q and %s are the snap name, developer, and price. Please wrap the translation at 80 characters.
	fmt.Fprintf(Stdout, i18n.G(`Please re-enter your Ubuntu One password to purchase %q from %q
for %s. Press ctrl-c to cancel.`), snap.Name, snap.Publisher.Username, formatPrice(opts.Price, opts.Currency))
	fmt.Fprint(Stdout, "\n")

	err = requestLogin(user.Email)
	if err != nil {
		return err
	}

	_, err = cli.Buy(opts)
	if err != nil {
		if e, ok := err.(*client.Error); ok {
			switch e.Kind {
			case client.ErrorKindPaymentDeclined:
				return fmt.Errorf(i18n.G(`Sorry, your payment method has been declined by the issuer. Please review your
payment details at https://my.ubuntu.com/payment/edit and try again.`))
			}
		}
		return err
	}

	// TRANSLATORS: %q and %s are the same snap name. Please wrap the translation at 80 characters.
	fmt.Fprintf(Stdout, i18n.G(`Thanks for purchasing %q. You may now install it on any of your devices
with 'snap install %s'.`), snapName, snapName)
	fmt.Fprint(Stdout, "\n")

	return nil
}
