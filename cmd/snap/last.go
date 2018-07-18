// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"errors"
	"fmt"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type changeIDMixin struct {
	LastChangeType string `long:"last"`
	Positional     struct {
		ID changeID `positional-arg-name:"<id>"`
	} `positional-args:"yes"`
}

var changeIDMixinOptDesc = mixinDescs{
	"last": i18n.G("Select last change of given type (install, refresh, remove, try, auto-refresh, etc.). A question mark at the end of the type means to do nothing (instead of returning an error) if no change of the given type is found. Note the question mark could need protecting from the shell."),
}

var changeIDMixinArgDesc = []argDesc{{
	// TRANSLATORS: This needs to be wrapped in <>s.
	name: i18n.G("<change-id>"),
	// TRANSLATORS: This should probably not start with a lowercase letter.
	desc: i18n.G("Change ID"),
}}

// should not be user-visible, but keep it clear and polite because mistakes happen
var noChangeFoundOK = errors.New("no change found but that's ok")

func (l *changeIDMixin) GetChangeID(cli *client.Client) (string, error) {
	if l.Positional.ID == "" && l.LastChangeType == "" {
		return "", fmt.Errorf(i18n.G("please provide change ID or type with --last=<type>"))
	}

	if l.Positional.ID != "" {
		if l.LastChangeType != "" {
			return "", fmt.Errorf(i18n.G("cannot use change ID and type together"))
		}

		return string(l.Positional.ID), nil
	}

	kind := l.LastChangeType
	optional := false
	if l := len(kind) - 1; kind[l] == '?' {
		optional = true
		kind = kind[:l]
	}
	// our internal change types use "-snap" postfix but let user skip it and use short form.
	if kind == "refresh" || kind == "install" || kind == "remove" || kind == "connect" || kind == "disconnect" || kind == "configure" || kind == "try" {
		kind += "-snap"
	}
	changes, err := queryChanges(cli, &client.ChangesOptions{Selector: client.ChangesAll})
	if err != nil {
		return "", err
	}
	if len(changes) == 0 {
		if optional {
			return "", noChangeFoundOK
		}
		return "", fmt.Errorf(i18n.G("no changes found"))
	}
	chg := findLatestChangeByKind(changes, kind)
	if chg == nil {
		if optional {
			return "", noChangeFoundOK
		}
		return "", fmt.Errorf(i18n.G("no changes of type %q found"), l.LastChangeType)
	}

	return chg.ID, nil
}

func findLatestChangeByKind(changes []*client.Change, kind string) (latest *client.Change) {
	for _, chg := range changes {
		if chg.Kind == kind && (latest == nil || latest.SpawnTime.Before(chg.SpawnTime)) {
			latest = chg
		}
	}
	return latest
}
