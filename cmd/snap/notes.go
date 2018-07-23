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
	"github.com/snapcore/snapd/snap"
)

func getPriceString(prices map[string]float64, suggestedCurrency, status string) string {
	price, currency, err := getPrice(prices, suggestedCurrency)

	// If there are no prices, then the snap is free
	if err != nil {
		return ""
	}

	// If the snap is priced, but has been purchased
	if status == "available" {
		return i18n.G("bought")
	}

	return formatPrice(price, currency)
}

func formatPrice(val float64, currency string) string {
	return fmt.Sprintf("%.2f%s", val, currency)
}

// Notes encapsulate everything that might be interesting about a
// snap, in order to present a brief summary of it.
type Notes struct {
	Price            string
	SnapType         snap.Type
	Private          bool
	DevMode          bool
	JailMode         bool
	Classic          bool
	TryMode          bool
	Disabled         bool
	Broken           bool
	IgnoreValidation bool
}

func NotesFromChannelSnapInfo(ref *snap.ChannelSnapInfo) *Notes {
	return &Notes{
		DevMode: ref.Confinement == client.DevModeConfinement,
		Classic: ref.Confinement == client.ClassicConfinement,
	}
}

func NotesFromRemote(snp *client.Snap, resInfo *client.ResultInfo) *Notes {
	notes := &Notes{
		Private:  snp.Private,
		DevMode:  snp.Confinement == client.DevModeConfinement,
		Classic:  snp.Confinement == client.ClassicConfinement,
		SnapType: snap.Type(snp.Type),
	}
	if resInfo != nil {
		notes.Price = getPriceString(snp.Prices, resInfo.SuggestedCurrency, snp.Status)
	}

	return notes
}

func NotesFromLocal(snp *client.Snap) *Notes {
	return &Notes{
		SnapType:         snap.Type(snp.Type),
		Private:          snp.Private,
		DevMode:          snp.DevMode,
		Classic:          !snp.JailMode && (snp.Confinement == client.ClassicConfinement),
		JailMode:         snp.JailMode,
		TryMode:          snp.TryMode,
		Disabled:         snp.Status != client.StatusActive,
		Broken:           snp.Broken != "",
		IgnoreValidation: snp.IgnoreValidation,
	}
}

func NotesFromInfo(info *snap.Info) *Notes {
	return &Notes{
		SnapType: info.Type,
		Private:  info.Private,
		DevMode:  info.Confinement == client.DevModeConfinement,
		Classic:  info.Confinement == client.ClassicConfinement,
		Broken:   info.Broken != "",
	}
}

func (n *Notes) String() string {
	if n == nil {
		return ""
	}
	var ns []string

	switch n.SnapType {
	case "", snap.TypeApp:
		// nothing
	case snap.TypeOS:
		ns = append(ns, "core")
	default:
		ns = append(ns, string(n.SnapType))
	}
	if n.Disabled {
		// TRANSLATORS: if possible, a single short word
		ns = append(ns, i18n.G("disabled"))
	}

	if n.Price != "" {
		ns = append(ns, n.Price)
	}

	if n.DevMode {
		ns = append(ns, "devmode")
	}

	if n.JailMode {
		ns = append(ns, "jailmode")
	}

	if n.Classic {
		ns = append(ns, "classic")
	}

	if n.Private {
		// TRANSLATORS: if possible, a single short word
		ns = append(ns, i18n.G("private"))
	}

	if n.TryMode {
		ns = append(ns, "try")
	}

	if n.Broken {
		// TRANSLATORS: if possible, a single short word
		ns = append(ns, i18n.G("broken"))
	}

	if n.IgnoreValidation {
		ns = append(ns, i18n.G("ignore-validation"))
	}

	if len(ns) == 0 {
		return "-"
	}

	return strings.Join(ns, ",")
}
