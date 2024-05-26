// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2023 Canonical Ltd
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

package configcore_test

import (
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	. "gopkg.in/check.v1"
)

func (s *configcoreSuite) TestNilHandleWithStateHandlerPanic(c *C) {
	c.Assert(func() { configcore.AddWithStateHandler(nil, nil, nil) },
		Panics, "cannot have nil handle with addWithStateHandler if validatedOnlyStateConfig flag is not set")
}

func (r *configcoreSuite) TestConfigureUnknownOption(c *C) {
	conf := &mockConf{
		state: r.state,
		changes: map[string]interface{}{
			"unknown.option": "1",
		},
	}
	mylog.Check(configcore.Run(coreDev, conf))
	c.Check(err, ErrorMatches, `cannot set "core.unknown.option": unsupported system option`)
}
