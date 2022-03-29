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

package validity_test

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

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/validity"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type validitySuite struct {
	testutil.BaseTest
}

func (s *validitySuite) SetUpTest(c *C) {
	restore := osutil.MockMountInfo("")
	s.AddCleanup(restore)
}

var _ = Suite(&validitySuite{})

func (s *validitySuite) TestRunHappy(c *C) {
	var happyChecks []func() error
	var happyCheckRan int

	happyChecks = append(happyChecks, func() error {
		happyCheckRan += 1
		return nil
	})

	restore := validity.MockChecks(happyChecks)
	defer restore()

	err := validity.Check()
	c.Check(err, IsNil)
	c.Check(happyCheckRan, Equals, 1)
}

func (s *validitySuite) TestRunNotHappy(c *C) {
	var unhappyChecks []func() error
	var unhappyCheckRan int

	unhappyChecks = append(unhappyChecks, func() error {
		unhappyCheckRan += 1
		return nil
	})

	restore := validity.MockChecks(unhappyChecks)
	defer restore()

	err := validity.Check()
	c.Check(err, IsNil)
	c.Check(unhappyCheckRan, Equals, 1)
}

func (s *validitySuite) TestUnexportedChecks(c *C) {
	// collect what funcs we run in validity.Check
	var runCheckers []string
	v := reflect.ValueOf(validity.Checks())
	for i := 0; i < v.Len(); i++ {
		v := v.Index(i)
		fname := runtime.FuncForPC(v.Pointer()).Name()
		pos := strings.LastIndexByte(fname, '.')
		checker := fname[pos+1:]
		if !strings.HasPrefix(checker, "check") {
			c.Fatalf(`%q in validity.Checks does not have "check" prefix`, checker)
		}
		runCheckers = append(runCheckers, checker)
	}

	// collect all "check*" functions
	goFiles, err := filepath.Glob("*.go")
	c.Assert(err, IsNil)
	fset := token.NewFileSet()

	var checkers []string
	for _, fn := range goFiles {
		f, err := parser.ParseFile(fset, fn, nil, 0)
		c.Assert(err, IsNil)
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
