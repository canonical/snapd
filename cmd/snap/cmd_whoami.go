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
	"bufio"
	"bytes"
	"encoding/json"
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

	RefreshSSHKeys bool `long:"refresh-ssh-keys"`
}

func init() {
	addCommand("whoami", shortWhoAmIHelp, longWhoAmIHelp, func() flags.Commander { return &cmdWhoAmI{} }, nil, nil)
}

func fromStoreFor(line, email string) bool {
	l := strings.SplitN(line, "# snapd ", 2)
	if len(l) != 2 {
		return false
	}
	lineInfo := struct {
		Origin string `json:"origin"`
		Email  string `json:"email"`
	}{}
	if err := json.Unmarshal([]byte(l[1]), &lineInfo); err != nil {
		return false
	}
	return lineInfo.Email == email
}

func refreshSSHKeys(email string) error {
	if release.OnClassic {
		return fmt.Errorf("cannot refresh keys: not supported on classic")
	}
	if email == "" {
		return fmt.Errorf("cannot refresh keys: no email")
	}

	// XXX: move this into DeviceMgr.Ensure() instead ?

	// FIXME: set auth context
	var storeCtx store.DeviceAndAuthContext
	sto := storeNew(nil, storeCtx)
	userInfo, err := sto.UserInfo(email)
	errPrefix := fmt.Sprintf("cannot refresh keys for %q: ", email)
	if err != nil {
		return fmt.Errorf(errPrefix+"%s", err)
	}
	if len(userInfo.SSHKeys) == 0 {
		return fmt.Errorf(errPrefix + "no ssh keys found")
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf(errPrefix+"%s", err)
	}
	authKeys := filepath.Join(homeDir, ".ssh/authorized_keys")

	// update auth-keys
	buffer := bytes.NewBuffer(nil)
	fin, err := os.Open(authKeys)
	if err != nil {
		return fmt.Errorf(errPrefix+"%s", err)
	}
	defer fin.Close()

	// read old authorized_keys file into a membuffer but skip
	// anything that came from the store for this email
	scanner := bufio.NewScanner(fin)
	for scanner.Scan() {
		line := scanner.Text()
		if fromStoreFor(line, email) {
			continue
		}
		if _, err := buffer.WriteString(line + "\n"); err != nil {
			return fmt.Errorf(errPrefix+"%s", err)
		}
	}
	for _, k := range userInfo.SSHKeys {
		// FIXME: userInfo.SSHKeys seems to contain "\n"
		k = strings.TrimSpace(k)
		line := fmt.Sprintf(`%s # snapd {"origin":"store","email":%q}`, k, email)
		if _, err := buffer.WriteString(line + "\n"); err != nil {
			return fmt.Errorf(errPrefix+"%s", err)
		}
	}
	// EnsureFileState will only update if anything changed
	authFileState := osutil.MemoryFileState{
		Content: buffer.Bytes(),
		Mode:    0600,
	}
	err = osutil.EnsureFileState(authKeys, &authFileState)
	if err != nil && err != osutil.ErrSameState {
		return fmt.Errorf(errPrefix+"%s", err)
	}
	if err == osutil.ErrSameState {
		fmt.Fprintf(Stdout, "No updated ssh keys for %s\n", email)
	} else {
		fmt.Fprintf(Stdout, "Updated ssh keys for %s\n", email)
	}

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
	if cmd.RefreshSSHKeys {
		return refreshSSHKeys(email)
	}

	if email == "" {
		// just printing nothing looks weird (as if something had gone wrong)
		email = "-"
	}
	fmt.Fprintln(Stdout, i18n.G("email:"), email)
	return nil
}
