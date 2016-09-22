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

package ctlcmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/hookstate"
)

type setCommand struct {
	baseCommand

	Positional struct {
		ConfValues []string `positional-arg-name:"key=value" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

type cachedTransaction struct{}

var shortSetHelp = i18n.G("Set snap configuration")
var longSetHelp = i18n.G(`
The set command changes the provided configuration options as requested. For
example:

    $ snapctl set username=joe password=$PASSWORD

All configuration changes are persisted at once, and only after the hook returns
successfully.`)

func init() {
	addCommand("set", shortSetHelp, longSetHelp, func() command { return &setCommand{} })
}

func (s *setCommand) Execute(args []string) error {
	if s.context() == nil {
		return fmt.Errorf("cannot set without a context")
	}

	// Extract the transaction from the context. If none, make one.
	s.context().Lock()
	transaction, ok := s.context().Cached(cachedTransaction{}).(*configstate.Transaction)
	s.context().Unlock()
	if !ok {
		var err error
		transaction, err = initializeTransaction(s.context())
		if err != nil {
			return fmt.Errorf("cannot initialize transaction: %s", err)
		}
	}

	for _, patchValue := range s.Positional.ConfValues {
		parts := strings.SplitN(patchValue, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf(i18n.G("invalid parameter: %q (want key=value)"), patchValue)
		}
		key := parts[0]
		var value interface{}
		err := json.Unmarshal([]byte(parts[1]), &value)
		if err != nil {
			// Not valid JSON-- just save the string as-is.
			value = parts[1]
		}

		transaction.Set(s.context().SnapName(), key, value)
	}

	s.context().Lock()
	s.context().Cache(cachedTransaction{}, transaction)
	s.context().Unlock()

	return nil
}

func initializeTransaction(context *hookstate.Context) (*configstate.Transaction, error) {
	state := context.State()
	state.Lock()
	defer state.Unlock()

	transaction, err := configstate.NewTransaction(state)
	if err != nil {
		return nil, err
	}

	context.OnDone(func() error {
		state.Lock()
		defer state.Unlock()
		transaction.Commit()
		return nil
	})

	return transaction, nil
}
