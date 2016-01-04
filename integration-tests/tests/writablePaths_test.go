// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015 Canonical Ltd
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

package tests

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

const writablePathsListFile = "/etc/system-image/writable-paths"

var _ = check.Suite(&writablePathsSuite{})

type writablePathsSuite struct {
	common.SnappySuite
}

var IsWritable check.Checker = &isWritable{}

type isWritable struct {
}

func (is *isWritable) Info() *check.CheckerInfo {
	return &check.CheckerInfo{Name: "IsWritable", Params: []string{"path"}}
}

func (is *isWritable) Check(params []interface{}, names []string) (result bool, error string) {
	if path, ok := params[0].(string); ok {
		filename := filepath.Join(path, "tmpfile")

		cmd := exec.Command("sudo", "touch", filename)
		rmCmd := exec.Command("sudo", "rm", filename)
		defer rmCmd.Run()

		if _, err := cmd.CombinedOutput(); err == nil {
			result = true
		} else {
			error = fmt.Sprintf("Error creating file %s", filename)
		}
	} else {
		error = fmt.Sprintf("First param of checker %v is of type %T and it should be a string", params[0], params[0])
	}
	return result, error
}

func (s *writablePathsSuite) TestWritablePathsAreWritable(c *check.C) {
	for writablePath := range generateWritablePaths(c) {
		c.Logf("Checking if %s is writable", writablePath)
		c.Check(writablePath, IsWritable)
	}
}

func generateWritablePaths(c *check.C) chan string {
	ch := make(chan string)

	go func() {
		file, err := os.Open(writablePathsListFile)

		c.Assert(err, check.IsNil,
			check.Commentf("Error reading writable files list %s", writablePathsListFile))

		defer file.Close()

		reader := bufio.NewReader(file)
		scanner := bufio.NewScanner(reader)

		scanner.Split(bufio.ScanLines)

		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) > 0 && fields[0] != "#" {
				if src, err := os.Stat(fields[0]); err == nil && src.IsDir() {
					ch <- fields[0]
				}
			}
		}
		close(ch)
	}()

	return ch
}
