// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package main_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"

	snapd "github.com/snapcore/snapd/cmd/snapd"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type snapdSuite struct {
	tmpdir string
}

var _ = Suite(&snapdSuite{})

func (s *snapdSuite) SetUpTest(c *C) {
	s.tmpdir = c.MkDir()
	for _, d := range []string{"/var/lib/snapd", "/run"} {
		err := os.MkdirAll(filepath.Join(s.tmpdir, d), 0755)
		c.Assert(err, IsNil)
	}
	dirs.SetRootDir(s.tmpdir)
}

func (s *snapdSuite) TestSanityFailGoesIntoDegradedMode(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	sanityErr := fmt.Errorf("foo failed")
	sanityCalled := make(chan bool)
	sanityClosed := false
	restore = snapd.MockSanityCheck(func() error {
		if !sanityClosed {
			sanityClosed = true
			close(sanityCalled)
		}
		return sanityErr
	})
	defer restore()

	restore = snapd.MockCheckRunningConditionsRetryDelay(10 * time.Millisecond)
	defer restore()

	// run the daemon
	ch := make(chan os.Signal)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := snapd.Run(ch)
		c.Check(err, IsNil)
	}()

	sanityCheckWasRun := false
	select {
	case <-time.After(5 * time.Second):
	case _, stillOpen := <-sanityCalled:
		c.Assert(stillOpen, Equals, false)
		sanityCheckWasRun = true
	}
	c.Check(sanityCheckWasRun, Equals, true)
	c.Check(logbuf.String(), testutil.Contains, "system does not fully support snapd: foo failed")

	// verify that talking to the daemon yields the sanity error
	// message
	// disable keepliave as it would sometimes cause the daemon to be
	// blocked when closing connections during graceful shutdown
	cli := client.New(&client.Config{DisableKeepAlive: true})
	_, err := cli.Abort("123")
	c.Check(err, ErrorMatches, "system does not fully support snapd: foo failed")

	// verify that the sysinfo command is still available
	_, err = cli.SysInfo()
	c.Check(err, IsNil)

	// stop the daemon
	close(ch)
	wg.Wait()
}
