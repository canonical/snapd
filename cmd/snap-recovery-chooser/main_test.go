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

package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-recovery-chooser"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type cmdSuite struct {
	testutil.BaseTest

	stdout, stderr bytes.Buffer
}

var _ = Suite(&cmdSuite{})

func (s *cmdSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	_, r := logger.MockLogger()
	s.AddCleanup(r)
	r = main.MockStdStreams(&s.stdout, &s.stderr)
	s.AddCleanup(r)
}

func (s *cmdSuite) TestRunUIHappy(c *C) {
	mockCmd := testutil.MockCommand(c, "tool", `
echo '{"id": "previous/20190917"}'
`)
	defer mockCmd.Restore()

	m := &main.Menu{
		Description: "Start into a previous version:",
		Entries: []main.Entry{
			{
				ID:   "previous/20190917",
				Text: "20190917 (last used 2019-09-17 21:07)",
			}, {
				ID:   "previous/20190901",
				Text: "20190917 (never used)",
			},
		},
	}

	action, err := main.RunUI(mockCmd.Exe(), m)
	c.Assert(err, IsNil)
	c.Assert(action, Equals, "previous/20190917")
}

func (s *cmdSuite) TestRunUIBadJSON(c *C) {
	mockCmd := testutil.MockCommand(c, "tool", `
echo 'garbage'
`)
	defer mockCmd.Restore()

	m := &main.Menu{
		Description: "Start into a previous version:",
		Entries: []main.Entry{
			{
				ID:   "something",
				Text: "else",
			},
		},
	}

	action, err := main.RunUI(mockCmd.Exe(), m)
	c.Assert(err, ErrorMatches, "cannot decode response: .*")
	c.Assert(action, Equals, "")
}

func (s *cmdSuite) TestRunUIToolErr(c *C) {
	mockCmd := testutil.MockCommand(c, "tool", `
echo foo
exit 22
`)
	defer mockCmd.Restore()

	m := &main.Menu{
		Description: "Start into a previous version:",
		Entries: []main.Entry{
			{
				ID:   "something",
				Text: "else",
			},
		},
	}

	_, err := main.RunUI(mockCmd.Exe(), m)
	c.Assert(err, ErrorMatches, "cannot obtain output of the UI process: exit status 22")
}

func (s *cmdSuite) TestRunUIInputJSON(c *C) {
	d := c.MkDir()
	tf := filepath.Join(d, "json-input")
	mockCmd := testutil.MockCommand(c, "tool", fmt.Sprintf(`
cat > %s
echo '{"id": "something"}'
`, tf))
	defer mockCmd.Restore()

	m := &main.Menu{
		Description: "Start into a previous version:",
		Entries: []main.Entry{
			{
				ID:   "something",
				Text: "else",
			}, {
				ID:   "nested",
				Text: "nested menu",
				Submenu: &main.Menu{
					Entries: []main.Entry{
						{
							ID:   "nested/something",
							Text: "nested else",
						},
					},
				},
			},
		},
	}

	_, err := main.RunUI(mockCmd.Exe(), m)
	c.Assert(err, IsNil)

	data, err := ioutil.ReadFile(tf)
	c.Assert(err, IsNil)
	var input *main.Menu
	err = json.Unmarshal(data, &input)
	c.Assert(err, IsNil)

	c.Assert(input, DeepEquals, m)
}

func (s *cmdSuite) TestStdoutUI(c *C) {
	m := &main.Menu{
		Description: "Start into a previous version:",
		Entries: []main.Entry{
			{
				ID:   "20190917",
				Text: "20190917 (last used 2019-09-17 21:07)",
			}, {
				ID:   "20190901",
				Text: "20190917 (never used)",
			},
		},
	}
	var buf bytes.Buffer
	err := main.OutputUI(&buf, m)
	c.Assert(err, IsNil)

	var out *main.Menu

	err = json.Unmarshal(buf.Bytes(), &out)
	c.Assert(err, IsNil)
	c.Assert(out, DeepEquals, m)
}

func (s *cmdSuite) TestMainChooserWithTool(c *C) {
	var action string

	mockCmd := testutil.MockCommand(c, "tool", `
echo '{"id": "self-test"}'
`)
	defer mockCmd.Restore()
	r := main.MockToolPath(func() (string, error) {
		return mockCmd.Exe(), nil
	})
	defer r()
	r = main.MockExecuteAction(func(act string) error {
		action = act
		return nil
	})
	defer r()

	err := main.Chooser()
	c.Assert(err, IsNil)
	c.Assert(action, Equals, "self-test")
}

func (s *cmdSuite) TestMainChooserToolNotFound(c *C) {
	d := c.MkDir()
	mf := filepath.Join(d, "marker")
	err := ioutil.WriteFile(mf, nil, 0644)
	c.Assert(err, IsNil)

	notATool := filepath.Join(d, "not-a-tool")

	r := main.MockDefaultMarkerFile(mf)
	defer r()
	r = main.MockToolPath(func() (string, error) {
		return notATool, nil
	})
	defer r()
	r = main.MockExecuteAction(func(_ string) error {
		return fmt.Errorf("unexpected call")
	})
	defer r()

	err = main.Chooser()
	c.Assert(err, IsNil)

	c.Assert(mf, testutil.FileAbsent)
}
