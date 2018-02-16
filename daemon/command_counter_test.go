// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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

package daemon

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"path/filepath"

	"gopkg.in/check.v1"
)

func countCommandDeclsIn(c *check.C, filename string, comment check.CommentInterface) int {
	// NOTE: there's probably a
	// better/easier way of doing this (patches welcome)
	//
	// Another note: the code below will find any and all variable
	// declaration that have a &Command{} on the right hand. This
	// is what we're currently using to declare the handlers that
	// fill the api list; it also counts Command{}, and
	// multi-variable (var foo, ... = Command{}, ...) just to
	// future-proof it a little bit, but it's still supposed to be
	// very restrictive. In particular I can think of different
	// ways of doing things that won't be counted by the code
	// below, i.e. the code below can still give false positives
	// by counting too few command instances, e.g. if they're
	// added directly to the api list, or if they're declared in a
	// function or secondary slice or ... but as it stands I can't
	// think of a way for it to give false negatives.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filename, nil, 0)
	c.Assert(err, check.IsNil, comment)

	found := 0

	ast.Inspect(f, func(n ast.Node) bool {
		var vs *ast.ValueSpec
		switch n := n.(type) {
		case *ast.ValueSpec:
			// a ValueSpec is a constant or variable
			// child of GenDecl
			vs = n
		case *ast.File:
			// yes we want to recurse into the file
			return true
		case *ast.GenDecl:
			// and we recurse into the toplevel GenDecls
			// (note a GenDecl can't contain a GenDecl)
			return true
		default:
			// don't recurse into anything else
			return false
		}
		// foo, bar = Command{}, Command{} -> two v.Values
		for i, v := range vs.Values {
			// note we loop over values, so empty declarations aren't counted
			if vs.Names[i].Name == "_" {
				// don't count "var _ = &Command{}"
				continue
			}
			// a Command{} is a composite literal; check for that
			x, ok := v.(*ast.CompositeLit)
			if !ok {
				// it might be a &Command{} instead
				// the & in &foo{} is an unary expression
				y, ok := v.(*ast.UnaryExpr)
				// (and yes the & in &foo{} is token.AND)
				if !ok || y.Op != token.AND {
					continue
				}
				// again check for Command{} (composite literal)
				x, ok = y.X.(*ast.CompositeLit)
				if !ok {
					continue
				}
			}
			// ok, x is a composite literal, ie foo{}.
			// the foo in foo{} is an Ident
			z, ok := x.Type.(*ast.Ident)
			if !ok {
				continue
			}
			if z.Name == "Command" {
				// gotcha!
				found++
			}
		}
		return false
	})

	return found
}

type cmdCounterSuite struct{}

var _ = check.Suite(&cmdCounterSuite{})

type commandDeclCounterTableT struct {
	desc    string
	count   int
	content string
}

var commandDeclCounterTable = []commandDeclCounterTableT{
	{"counts top-level vars", 4, `
var won, too = &Command{}, Command{}
var tri = &Command{}
var foh = Command{}
`},
	{"count top-level vars in groups", 4, `
var (
    won, too = &Command{}, Command{}
    tri = &Command{}
    foh = Command{}
)
`},
	{"does *not* count these (should it?)", 0, `
var wonP, tooP *Command
var wonD, tooD Command
var triP *Command
var triD Command
`},
	{"not in groups either", 0, `
var (
    wonP, tooP *Command
    wonD, tooD Command
    triP *Command
    triD Command
)
`},

	{"does not count empty decls", 0, `
var _, _ = &Command{}, Command{}
var _ = &Command{}
var _ = Command{}
`},
	{"does not count empty decls in groups", 0, `
var (
    _, _ = &Command{}, Command{}
    _ = &Command{}
    _ = Command{}
)
`},
	{"does not count things built in functions", 0, `
func won() *Command {
    return &Command{}
}
func too() *Command {
    var x = &Command{}
    return x
}
func tri() Command {
    return Command{}
}
func foh() Command {
    var x = Command{}
    return x
}
`},
	{"does not count things built in lists", 0, `
var won = []Command{{}, {}, {}}
var too = []Command{Command{}, Command{}}
var tri = []*Command{nil, nil, nil}
var foh = []*Command{{}, {}, {}}
var fai = []*Command{&Command{}, &Command{}}
`},
	{"does not count things built in lists in groups", 0, `
var (
    won = []Command{{}, {}, {}}
    too = []Command{Command{}, Command{}}
    tri = []*Command{nil, nil, nil}
    foh = []*Command{{}, {}, {}}
    fai = []*Command{&Command{}, &Command{}}
)
`},
}

func (cmdCounterSuite) TestCommandDeclCounter(c *check.C) {
	d := c.MkDir()

	for i, t := range commandDeclCounterTable {
		fn := filepath.Join(d, fmt.Sprintf("a_%02d.go", i))
		comm := check.Commentf(t.desc)
		c.Assert(ioutil.WriteFile(fn, []byte("package huh"+t.content), 0644), check.IsNil, comm)
		n := countCommandDeclsIn(c, fn, comm)
		c.Check(n, check.Equals, t.count, comm)
	}
}
