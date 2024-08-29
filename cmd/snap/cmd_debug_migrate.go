// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/strutil"
)

type cmdMigrateHome struct {
	waitMixin

	Positional struct {
		Snaps []string `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addDebugCommand("migrate-home",
		"Migrate snaps' directory to ~/Snap.",
		"Migrate snaps' directory to ~/Snap.",
		func() flags.Commander {
			return &cmdMigrateHome{}
		}, nil, nil)
}

func (x *cmdMigrateHome) Execute(args []string) error {
	chgID, err := x.client.MigrateSnapHome(x.Positional.Snaps)
	if err != nil {
		msg, err := errorToCmdMessage("", "migrate-home", err, nil)
		if err != nil {
			return err
		}
		fmt.Fprintln(Stderr, msg)
		return nil
	}

	chg, err := x.wait(chgID)
	if err != nil {
		return err
	}

	var snaps []string
	if err := chg.Get("snap-names", &snaps); err != nil {
		return errors.New(`cannot get "snap-names" from change`)
	}

	if len(snaps) == 0 {
		return errors.New(`expected "migrate-home" change to have non-empty "snap-names"`)
	}

	msg := fmt.Sprintf("%s's home directory was migrated to ~/Snap\n", snaps[0])
	if len(snaps) > 1 {
		msg = fmt.Sprintf(i18n.G("%s migrated their home directories to ~/Snap\n"), strutil.Quoted(snaps))
	}

	fmt.Fprint(Stdout, msg)
	return nil
}
