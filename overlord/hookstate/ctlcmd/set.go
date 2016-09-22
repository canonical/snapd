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
		ConfValues []string `positional-arg-name:"<conf value>" description:"configuration value (key=value)" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

var shortSetHelp = i18n.G("Set snap configuration")
var longSetHelp = i18n.G(`
The set command sets configuration parameters for the snap determined via the
SNAP_CONTEXT environment variable. This command accepts a number of key=value
pairs of parameters, for example:

$ snapctl set foo=bar baz=qux`)

func init() {
	addCommand("set", shortSetHelp, longSetHelp, func() command { return &setCommand{} })
}

func (s *setCommand) Execute(args []string) error {
	if s.context() == nil {
		return fmt.Errorf("cannot set without a context")
	}

	// Extract the transaction from the context. If none, make one.
	s.context().Lock()
	transaction, ok := s.context().Cached("transaction").(*configstate.Transaction)
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
			return fmt.Errorf(i18n.G("invalid configuration: %q (want key=value)"), patchValue)
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
	s.context().Cache("transaction", transaction)
	s.context().Unlock()

	return nil
}

func initializeTransaction(context *hookstate.Context) (*configstate.Transaction, error) {
	transaction, err := configstate.NewTransaction(context.State())
	if err != nil {
		return nil, err
	}

	context.OnDone(func() error {
		transaction.Commit()
		return nil
	})

	return transaction, nil
}
