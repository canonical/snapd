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

package snappy

//go:generate ../update-pot

import (
	"github.com/gosexy/gettext"
)

// Note that we have to use dgettext() here because we are a library
// and we can not use getext.Textdomain() as this would override the
// applications default
const TEXTDOMAIN = "snappy"

// G is the shorthand for Gettext
var G = func(msgid string) string {
	return gettext.DGettext(TEXTDOMAIN, msgid)
}

// NG is the shorthand for NGettext
var NG = func(msgid string, msgid_plural string, n uint64) string {
	return gettext.DNGettext(TEXTDOMAIN, msgid, msgid_plural, n)
}

func init() {
	gettext.SetLocale(gettext.LC_ALL, "")
}
