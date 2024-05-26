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

package syscheck_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/syscheck"
	"github.com/snapcore/snapd/testutil"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type syscheckSuite struct {
	testutil.BaseTest
}

func (s *syscheckSuite) SetUpTest(c *C) {
	restore := osutil.MockMountInfo("")
	s.AddCleanup(restore)
}

var _ = Suite(&syscheckSuite{})

func (s *syscheckSuite) TestRunHappy(c *C) {
	var happyChecks []func() error
	var happyCheckRan int

	happyChecks = append(happyChecks, func() error {
		happyCheckRan += 1
		return nil
	})

	restore := syscheck.MockChecks(happyChecks)
	defer restore()
	mylog.Check(syscheck.CheckSystem())
	c.Check(err, IsNil)
	c.Check(happyCheckRan, Equals, 1)
}

func (s *syscheckSuite) TestRunNotHappy(c *C) {
	var unhappyChecks []func() error
	var unhappyCheckRan int

	unhappyChecks = append(unhappyChecks, func() error {
		unhappyCheckRan += 1
		return nil
	})

	restore := syscheck.MockChecks(unhappyChecks)
	defer restore()
	mylog.Check(syscheck.CheckSystem())
	c.Check(err, IsNil)
	c.Check(unhappyCheckRan, Equals, 1)
}

func (s *syscheckSuite) TestUnexportedChecks(c *C) {
	// collect what funcs we run in syscheck.CheckSystem
	var runCheckers []string
	v := reflect.ValueOf(syscheck.Checks())
	for i := 0; i < v.Len(); i++ {
		v := v.Index(i)
		fname := runtime.FuncForPC(v.Pointer()).Name()
		pos := strings.LastIndexByte(fname, '.')
		checker := fname[pos+1:]
		if !strings.HasPrefix(checker, "check") {
			c.Fatalf(`%q in syscheck.Checks does not have "check" prefix`, checker)
		}
		runCheckers = append(runCheckers, checker)
	}

	// collect all "check*" functions
	goFiles := mylog.Check2(filepath.Glob("*.go"))

	fset := token.NewFileSet()

	var checkers []string
	for _, fn := range goFiles {
		f := mylog.Check2(parser.ParseFile(fset, fn, nil, 0))

		ast.Inspect(f, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.File:
				return true
			case *ast.FuncDecl:
				name := x.Name.Name
				if strings.HasPrefix(name, "check") {
					checkers = append(checkers, name)
				}
				return false
			default:
				return false
			}
		})
	}

	sort.Strings(checkers)
	sort.Strings(runCheckers)

	c.Check(checkers, DeepEquals, runCheckers)
}
