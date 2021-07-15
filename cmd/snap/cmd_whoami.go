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
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/store"
)

var shortWhoAmIHelp = i18n.G("Show the email the user is logged in with")
var longWhoAmIHelp = i18n.G(`
The whoami command shows the email the user is logged in with.
`)

type cmdWhoAmI struct {
	clientMixin

	// XXX: rename to --{resync,overwrite,rewrite} or similar?
	RefreshKeys bool `long:"refresh-keys"`
}

func init() {
	addCommand("whoami", shortWhoAmIHelp, longWhoAmIHelp, func() flags.Commander { return &cmdWhoAmI{} }, nil, nil)
}

func refreshKeys(email string) error {
	if release.OnClassic {
		return fmt.Errorf("cannot refresh keys: not supported on classic")
	}
	if email == "" {
		return fmt.Errorf("cannot refresh keys: no email")
	}

	// FIXME: set auth context
	var storeCtx store.DeviceAndAuthContext
	sto := storeNew(nil, storeCtx)
	userInfo, err := sto.UserInfo(email)
	if err != nil {
		return fmt.Errorf("cannot refresh keys for %q: %s", email, err)
	}
	if len(userInfo.SSHKeys) == 0 {
		return fmt.Errorf("cannot reresh keys for %q: no ssh keys found", email)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("cannot refresh keys for %q: %s", email, err)
	}
	sshDir := filepath.Join(homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return fmt.Errorf("cannot refresh %s: %s", sshDir, err)
	}
	authKeys := filepath.Join(sshDir, "authorized_keys")
	authKeysContent := strings.Join(userInfo.SSHKeys, "\n")
	if err := osutil.AtomicWriteFile(authKeys, []byte(authKeysContent), 0600, 0); err != nil {
		return err
	}
	fmt.Fprintf(Stdout, "Wrote %v keys for %s\n", len(userInfo.SSHKeys), email)
	return nil
}

func (cmd cmdWhoAmI) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	email, err := cmd.client.WhoAmI()
	if err != nil {
		return err
	}
	if cmd.RefreshKeys {
		return refreshKeys(email)
	}

	if email == "" {
		// just printing nothing looks weird (as if something had gone wrong)
		email = "-"
	}
	fmt.Fprintln(Stdout, i18n.G("email:"), email)
	return nil
}
