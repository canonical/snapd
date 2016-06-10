// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package i18n

//go:generate update-pot

import (
	"github.com/gosexy/gettext"
)

// TEXTDOMAIN is the message domain used by snappy; see dgettext(3)
// for more information.
//
// Note that we have to use dgettext() here because we are a library
// and we can not use getext.Textdomain() as this would override the
// applications default
var TEXTDOMAIN = "snappy"

// G is the shorthand for Gettext
func G(msgid string) string {
	return gettext.DGettext(TEXTDOMAIN, msgid)
}

// NG is the shorthand for NGettext
func NG(msgid string, msgidPlural string, n uint64) string {
	return gettext.DNGettext(TEXTDOMAIN, msgid, msgidPlural, n)
}

func init() {
	gettext.SetLocale(gettext.LC_ALL, "")
}
