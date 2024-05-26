// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/testutil"
)

type swapCfgSuite struct {
	configcoreSuite

	configSwapFile string
}

var _ = Suite(&swapCfgSuite{})

func (s *swapCfgSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	s.systemctlArgs = nil
	s.configSwapFile = filepath.Join(dirs.GlobalRootDir, "/etc/default/swapfile")
	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755))

	mylog.Check(os.WriteFile(filepath.Join(dirs.GlobalRootDir, "/etc/environment"), nil, 0644))

}

func (s *swapCfgSuite) TestConfigureSwapSizeOnlyWhenChanged(c *C) {
	mylog.
		// set it to 1M initially
		Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				"swap.size": "1048576",
			},
		}))


	c.Check(s.configSwapFile, testutil.FileEquals, `FILE=/var/tmp/swapfile.swp
SIZE=1
`)

	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"stop", "swapfile.service"},
		{"show", "--property=ActiveState", "swapfile.service"},
		{"start", "swapfile.service"},
	})

	s.systemctlArgs = nil
	mylog.

		// running it with the same changes as conf results in no calls to systemd
		Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"swap.size": "1048576",
			},
			changes: map[string]interface{}{
				"swap.size": "1048576",
			},
		}))


	c.Check(s.configSwapFile, testutil.FileEquals, `FILE=/var/tmp/swapfile.swp
SIZE=1
`)

	c.Check(s.systemctlArgs, HasLen, 0)
}

func (s *swapCfgSuite) TestConfigureSwapSize(c *C) {
	mylog.
		// set it to 1M initially
		Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			changes: map[string]interface{}{
				"swap.size": "1048576",
			},
		}))


	c.Check(s.configSwapFile, testutil.FileEquals, `FILE=/var/tmp/swapfile.swp
SIZE=1
`)

	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"stop", "swapfile.service"},
		{"show", "--property=ActiveState", "swapfile.service"},
		{"start", "swapfile.service"},
	})

	s.systemctlArgs = nil
	mylog.

		// now change it to empty
		Check(configcore.FilesystemOnlyRun(coreDev, &mockConf{
			state: s.state,
			conf: map[string]interface{}{
				"swap.size": "1048576",
			},
			changes: map[string]interface{}{
				"swap.size": "",
			},
		}))


	c.Check(s.configSwapFile, testutil.FileEquals, `FILE=/var/tmp/swapfile.swp
SIZE=0
`)

	c.Check(s.systemctlArgs, DeepEquals, [][]string{
		{"stop", "swapfile.service"},
		{"show", "--property=ActiveState", "swapfile.service"},
		{"start", "swapfile.service"},
	})
}

func (s *swapCfgSuite) TestSwapSizeNumberFormats(c *C) {
	tt := []struct {
		sizeStr     string
		sizeFileStr string
		err         string
	}{
		{
			sizeStr:     "1073741824",
			sizeFileStr: "1024",
		},
		{
			sizeStr:     "1024M",
			sizeFileStr: "1024",
		},
		{
			sizeStr:     "1G",
			sizeFileStr: "1024",
		},
		{
			sizeStr: "1048576K",
			err:     "invalid suffix \"K\"",
		},
		{
			sizeStr: "1073741824.4",
			err:     "invalid suffix \".4\"",
		},
		{
			sizeStr: "1",
			err:     "swap size setting must be at least one megabyte",
		},
		{
			sizeStr: "1073741825",
			err:     "swap size setting must be an integer number of megabytes",
		},
	}
	mylog.Check(os.MkdirAll(filepath.Dir(s.configSwapFile), 0755))


	for _, t := range tt {
		conf := configcore.PlainCoreConfig(map[string]interface{}{
			"swap.size": t.sizeStr,
		})
		mylog.Check(configcore.FilesystemOnlyApply(coreDev, dirs.GlobalRootDir, conf))
		if t.err != "" {
			c.Assert(err, ErrorMatches, t.err)
		} else {

			c.Check(s.configSwapFile, testutil.FileEquals, fmt.Sprintf(`FILE=/var/tmp/swapfile.swp
SIZE=%s
`, t.sizeFileStr))
		}
	}
}

func (s *swapCfgSuite) TestSwapSizeFilesystemOnlyApply(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"swap.size": "1024M",
	})
	mylog.

		// with no swapfile config in place we use sensible defaults
		Check(os.MkdirAll(filepath.Dir(s.configSwapFile), 0755))


	c.Assert(configcore.FilesystemOnlyApply(coreDev, dirs.GlobalRootDir, conf), IsNil)

	c.Check(s.configSwapFile, testutil.FileEquals, `FILE=/var/tmp/swapfile.swp
SIZE=1024
`)
}

func (s *swapCfgSuite) TestSwapSizeFilesystemOnlyApplyExistingConfig(c *C) {
	conf := configcore.PlainCoreConfig(map[string]interface{}{
		"swap.size": "1024M",
	})
	mylog.

		// we use the value from the config file if FILE is specified in the
		// existing config file
		Check(os.MkdirAll(filepath.Dir(s.configSwapFile), 0755))

	mylog.Check(os.WriteFile(s.configSwapFile, []byte(`FILE=/var/tmp/other-swapfile.swp
SIZE=0`), 0644))

	mylog.Check(configcore.FilesystemOnlyApply(coreDev, dirs.GlobalRootDir, conf))


	c.Check(s.configSwapFile, testutil.FileEquals, `FILE=/var/tmp/other-swapfile.swp
SIZE=1024
`)
}
