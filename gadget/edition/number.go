// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package edition

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/ddkwork/golibrary/mylog"
)

// Number can hold (and unmarshal) an edition number, used in
// gadget.yaml and kernel.yaml to control whether updates should be
// applied.
type Number uint32

func (e *Number) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var es string
	mylog.Check(unmarshal(&es))

	u := mylog.Check2(strconv.ParseUint(es, 10, 32))

	*e = Number(u)
	return nil
}
