// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nomanagers
// +build !nomanagers

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
	"encoding/json"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type storeSuite struct {
	configcoreSuite
}

var _ = Suite(&storeSuite{})

func (s *storeSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)
	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755))

	mylog.Check(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/etc/environment"), nil, 0644))

}

func (s *storeSuite) TestStoreAccessHappy(c *C) {
	mylog.Check(configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"store.access": "offline",
		},
	}))


	f := mylog.Check2(os.Open(dirs.SnapRepairConfigFile))

	defer f.Close()

	var repairConfig configcore.RepairConfig
	mylog.Check(json.NewDecoder(f).Decode(&repairConfig))


	c.Check(repairConfig.StoreOffline, Equals, true)
}

func (s *storeSuite) TestStoreAccessUnhappy(c *C) {
	mylog.Check(configcore.Run(coreDev, &mockConf{
		state: s.state,
		changes: map[string]interface{}{
			"store.access": "invalid",
		},
	}))
	c.Assert(err, ErrorMatches, ".*store access can only be set to 'offline'")
}

func (s *storeSuite) TestFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"store.access": "offline",
	})

	tmpDir := c.MkDir()
	c.Assert(configcore.FilesystemOnlyApply(coreDev, tmpDir, conf), IsNil)

	f := mylog.Check2(os.Open(dirs.SnapRepairConfigFileUnder(tmpDir)))

	defer f.Close()

	var repairConfig configcore.RepairConfig
	mylog.Check(json.NewDecoder(f).Decode(&repairConfig))


	c.Check(repairConfig.StoreOffline, Equals, true)
}
