// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers

/*
 * Copyright (C) 2021 Canonical Ltd
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
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type earlySuite struct {
	configcoreSuite
}

var _ = Suite(&earlySuite{})

func (s *earlySuite) TestEarly(c *C) {
	patch := map[string]interface{}{
		"experimental.parallel-instances": true,
		"experimental.user-daemons":       true,
		"service.ssh.disable":             true,
	}
	tr := &mockConf{state: s.state}
	mylog.Check(configcore.Early(coreDev, tr, patch))


	// only early options as described by flags earlyConfigFilters
	// were processed

	c.Check(tr.conf, DeepEquals, map[string]interface{}{
		"experimental.parallel-instances": true,
		"experimental.user-daemons":       true,
	})
	c.Check(features.ParallelInstances.IsEnabled(), Equals, true)
}
