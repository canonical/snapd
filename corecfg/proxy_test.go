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

package corecfg_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/corecfg"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/testutil"
)

type proxySuite struct {
	mockEtcEnvironment string
}

var _ = Suite(&proxySuite{})

func (s *proxySuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	err := os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, IsNil)
	s.mockEtcEnvironment = filepath.Join(dirs.GlobalRootDir, "/etc/environment")
}

func (s *proxySuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *proxySuite) TestConfigureProxy(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	for _, action := range []string{"http", "https", "ftp"} {
		mockSnapctl := testutil.MockCommand(c, "snapctl", fmt.Sprintf(`
if [ "$1" = "get" ] && [ "$2" = "proxy.%[1]s" ]; then
    echo "%[1]s://example.com"
fi
`, action))
		defer mockSnapctl.Restore()

		// populate with content
		err := ioutil.WriteFile(s.mockEtcEnvironment, []byte(`
PATH="/usr/bin"
`), 0644)
		c.Assert(err, IsNil)

		err = corecfg.Run()
		c.Assert(err, IsNil)

		content, err := ioutil.ReadFile(s.mockEtcEnvironment)
		c.Assert(err, IsNil)
		c.Check(string(content), Equals, fmt.Sprintf(`
PATH="/usr/bin"
%[1]s_proxy=%[1]s://example.com`, action))
	}
}
