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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

var (
	shortBuyHelp = i18n.G("Buy a snap")
	longBuyHelp  = i18n.G(`
The buy command buys a snap from the store.
`)
)

type cmdBuy struct {
	clientMixin
	Positional struct {
		SnapName remoteSnapName
	} `positional-args:"yes" required:"yes"`
}

func init() {
	cmd := addCommand("buy", shortBuyHelp, longBuyHelp, func() flags.Commander {
		return &cmdBuy{}
	}, map[string]string{}, []argDesc{{
		name: "<snap>",
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("Snap name"),
	}})
	cmd.hidden = true
}

func (x *cmdBuy) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return buySnap(x.client, string(x.Positional.SnapName))
}

func buySnap(cli *client.Client, snapName string) error {
	user := cli.LoggedInUser()
	if user == nil {
		return fmt.Errorf(i18n.G("You need to be logged in to purchase software. Please run 'snap login' and try again."))
	}

	if strings.ContainsAny(snapName, ":*") {
		return fmt.Errorf(i18n.G("cannot buy snap: invalid characters in name"))
	}

	snap, resultInfo := mylog.Check3(cli.FindOne(snapName))

	opts := &client.BuyOptions{
		SnapID:   snap.ID,
		Currency: resultInfo.SuggestedCurrency,
	}

	opts.Price, opts.Currency = mylog.Check3(getPrice(snap.Prices, opts.Currency))

	if snap.Status == "available" {
		return fmt.Errorf(i18n.G("cannot buy snap: it has already been bought"))
	}
	mylog.Check(cli.ReadyToBuy())

	// TRANSLATORS: %q, %q and %s are the snap name, developer, and price. Please wrap the translation at 80 characters.
	fmt.Fprintf(Stdout, i18n.G(`Please re-enter your Ubuntu One password to purchase %q from %q
for %s. Press ctrl-c to cancel.`), snap.Name, snap.Publisher.Username, formatPrice(opts.Price, opts.Currency))
	fmt.Fprint(Stdout, "\n")
	mylog.Check(requestLogin(cli, user.Email))

	_ = mylog.Check2(cli.Buy(opts))

	// TRANSLATORS: %q and %s are the same snap name. Please wrap the translation at 80 characters.
	fmt.Fprintf(Stdout, i18n.G(`Thanks for purchasing %q. You may now install it on any of your devices
with 'snap install %s'.`), snapName, snapName)
	fmt.Fprint(Stdout, "\n")

	return nil
}
