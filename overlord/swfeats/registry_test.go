// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package swfeats_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/swfeats"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type swfeatsSuite struct {
	testutil.BaseTest
}

var _ = Suite(&swfeatsSuite{})

func (s *swfeatsSuite) TestAddChange(c *C) {
	registry := swfeats.NewChangeKindRegistry()
	restore := swfeats.MockChangeKindRegistry(registry)
	defer restore()

	changeKind := swfeats.RegisterChangeKind("my-change")
	c.Assert(changeKind, Equals, "my-change")
}

func (s *swfeatsSuite) TestKnownChangeKinds(c *C) {
	registry := swfeats.NewChangeKindRegistry()
	restore := swfeats.MockChangeKindRegistry(registry)
	defer restore()

	myChange1 := swfeats.RegisterChangeKind("my-change1")
	c.Assert(myChange1, Equals, "my-change1")

	// Add the same change again to check that it isn't added
	// more than once
	myChange1 = swfeats.RegisterChangeKind("my-change1")
	c.Assert(myChange1, Equals, "my-change1")
	myChange2 := swfeats.RegisterChangeKind("my-change2")
	c.Assert(myChange2, Equals, "my-change2")
	changeKinds := swfeats.KnownChangeKinds()
	c.Assert(changeKinds, HasLen, 2)
	c.Assert(changeKinds, testutil.Contains, "my-change1")
	c.Assert(changeKinds, testutil.Contains, "my-change2")
}

func (s *swfeatsSuite) TestNewChangeTemplateKnown(c *C) {
	registry := swfeats.NewChangeKindRegistry()
	restore := swfeats.MockChangeKindRegistry(registry)
	defer restore()

	changeKind := swfeats.RegisterChangeKind("my-change-%s")
	changeKind2 := swfeats.RegisterChangeKind("my-change-%s")
	c.Assert(changeKind, Equals, changeKind2)
	kinds := swfeats.KnownChangeKinds()
	// Without possible values added, a templated change will generate
	// the template
	c.Assert(kinds, HasLen, 1)
	c.Assert(kinds, testutil.Contains, "my-change-%s")

	swfeats.AddChangeKindVariants(changeKind, []string{"1", "2", "3"})
	kinds = swfeats.KnownChangeKinds()
	c.Assert(kinds, HasLen, 3)
	c.Assert(kinds, testutil.Contains, "my-change-1")
	c.Assert(kinds, testutil.Contains, "my-change-2")
	c.Assert(kinds, testutil.Contains, "my-change-3")
}

func (s *swfeatsSuite) TestAddEnsure(c *C) {
	registry := swfeats.NewEnsureRegistry()
	restore := swfeats.MockEnsureRegistry(registry)
	defer restore()

	c.Assert(swfeats.KnownEnsures(), HasLen, 0)
	swfeats.RegisterEnsure("MyManager", "myFunction")
	knownEnsures := swfeats.KnownEnsures()
	c.Assert(knownEnsures, HasLen, 1)
	c.Assert(knownEnsures, testutil.Contains, swfeats.EnsureEntry{Manager: "MyManager", Function: "myFunction"})
}

func (s *swfeatsSuite) TestDuplicateAdd(c *C) {
	registry := swfeats.NewEnsureRegistry()
	restore := swfeats.MockEnsureRegistry(registry)
	defer restore()

	swfeats.RegisterEnsure("MyManager", "myFunction1")
	swfeats.RegisterEnsure("MyManager", "myFunction1")
	swfeats.RegisterEnsure("MyManager", "myFunction2")
	swfeats.RegisterEnsure("MyManager", "myFunction2")
	knownEnsures := swfeats.KnownEnsures()
	c.Assert(knownEnsures, HasLen, 2)
	c.Assert(knownEnsures, testutil.Contains, swfeats.EnsureEntry{Manager: "MyManager", Function: "myFunction1"})
	c.Assert(knownEnsures, testutil.Contains, swfeats.EnsureEntry{Manager: "MyManager", Function: "myFunction2"})
}

func (s *swfeatsSuite) TestCheckChangeRegistrations(c *C) {
	goFiles, err := getGoFiles("../../", "/snapstatetest/", "../../.git/", "../../vendor/", "../../tests/")
	if err != nil {
		c.Error("Could not find any go files in snapd directory")
	}
	var files []*ast.File
	fset := token.NewFileSet()
	for _, file := range goFiles {
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			c.Errorf("Failed to parse file %s: %v", file, err)
		}
		files = append(files, f)
	}
	// Collect variables names assigned with swfeats.RegChangeKind(...)
	changeKindRegVars := collectChangeRegAddVars(files)
	if len(changeKindRegVars) == 0 {
		c.Log("No variables assigned with swfeats.RegChangeKind(...) found, skipping test.")
		return
	}

	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			selExpr, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selExpr.Sel.Name != "NewChange" {
				return true
			}

			if len(call.Args) == 0 {
				c.Errorf("NewChange call with no arguments at %s", fset.Position(call.Pos()))
				return true
			}

			// The first argument to NewChange is the change kind
			firstArg := call.Args[0]

			// In the simplest case, the first parameter of NewChange is directly
			// a variable returned from the swfeats.RegChangeKind function call.
			// If so, the change kind is registered, so no need to continue.
			if exprContainsChangeRegVar(firstArg, changeKindRegVars) {
				return true
			}

			// The first parameter of NewChange might be a function call
			// (e.g. fmt.Sprintf). If that function anywhere contains a variable
			// returned from swfeats.RegChangeKind, then consider the change
			// kind registered and stop
			if callExpr, ok := firstArg.(*ast.CallExpr); ok {
				if exprContainsChangeRegVar(callExpr, changeKindRegVars) {
					return true
				}
			}

			// At this point, if firstArg is not a variable, fail the test.
			// If the firstArg is a string, failing is correct. This could
			// potentially be the wrong thing to do when faced with a more
			// complex scenario.
			firstArgIdent, isIdent := firstArg.(*ast.Ident)
			if !isIdent {
				c.Errorf("First argument to NewChange at %s does not appear to be properly registered using swfeats.RegChangeKind.", fset.Position(call.Pos()))
				return true
			}

			// Find the function containing this call
			fn := findFuncDecl(f, call.Pos())
			if fn == nil {
				c.Errorf("Could not find function containing NewChange call at %s", fset.Position(call.Pos()))
				return true
			}

			// If firstArg is present in the function call signature,
			// then this is a wrapper function and can be ignored.
			if isParam(fn, firstArgIdent.Name) {
				return true
			}

			// If firstArg is only assigned values from registered change kind variables,
			// then consider the change kind properly registered.
			if isVarAssignedFromChangeRegVars(fn, firstArgIdent.Name, changeKindRegVars) {
				return true
			} else {
				c.Errorf("NewChange call at %s does not appear to be properly registered using swfeats.RegChangeKind. The variable passed as a change kind (%q) should only be assigned values from the variable outputs of swfeats.RegChangeKind", fset.Position(call.Pos()), firstArgIdent.Name)
			}
			return true
		})
	}
}

func getGoFiles(dir string, excludePathsWithSubstring ...string) ([]string, error) {
	var goFiles []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "test.go") {
			return nil
		}
		for _, excludePath := range excludePathsWithSubstring {
			if strings.Contains(path, excludePath) {
				return nil
			}
		}
		goFiles = append(goFiles, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return goFiles, nil
}

// Extract variable names assigned with swfeats.RegChangeKind(...)
func collectChangeRegAddVars(files []*ast.File) map[string]struct{} {
	vars := make(map[string]struct{})

	for _, f := range files {
		for _, decl := range f.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.VAR {
				continue
			}

			for _, spec := range genDecl.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}

				for i, value := range valueSpec.Values {
					call, ok := value.(*ast.CallExpr)
					if !ok {
						continue
					}
					selExpr, ok := call.Fun.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					selX, ok := selExpr.X.(*ast.SelectorExpr)
					if !ok {
						continue
					}
					ident1, ok1 := selX.X.(*ast.Ident)
					ident2 := selX.Sel
					if !ok1 || ident1.Name != "swfeats" || ident2.Name != "ChangeReg" {
						continue
					}
					if selExpr.Sel.Name != "Add" {
						continue
					}

					// Get the variable name corresponding to this value
					if i < len(valueSpec.Names) {
						varName := valueSpec.Names[i].Name
						vars[varName] = struct{}{}
					}
				}
			}
		}
	}

	return vars
}

// Find function declaration containing a position
func findFuncDecl(file *ast.File, pos token.Pos) *ast.FuncDecl {
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if fn.Pos() <= pos && pos <= fn.End() {
			return fn
		}
	}
	return nil
}

func isParam(fn *ast.FuncDecl, varName string) bool {
	if fn == nil || fn.Type.Params == nil {
		return false
	}
	for _, field := range fn.Type.Params.List {
		for _, name := range field.Names {
			if name.Name == varName {
				return true
			}
		}
	}
	return false
}

// Helper: recursively check if an expression contains an Ident from changeKindRegVars
func exprContainsChangeRegVar(expr ast.Expr, changeKindRegVars map[string]struct{}) bool {
	switch e := expr.(type) {
	case *ast.Ident:
		_, ok := changeKindRegVars[e.Name]
		return ok
	case *ast.CallExpr:
		for _, arg := range e.Args {
			if exprContainsChangeRegVar(arg, changeKindRegVars) {
				return true
			}
		}
		return false
	case *ast.ParenExpr:
		return exprContainsChangeRegVar(e.X, changeKindRegVars)
	default:
		return false
	}
}

func isVarAssignedFromChangeRegVars(fn *ast.FuncDecl, varName string, changeKindRegVars map[string]struct{}) bool {
	missingAssignment := false
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok {
			return true
		}

		for i, lhs := range assign.Lhs {
			lhsIdent, ok := lhs.(*ast.Ident)
			if !ok || lhsIdent.Name != varName {
				continue
			}

			if i >= len(assign.Rhs) {
				continue
			}

			if !exprContainsChangeRegVar(assign.Rhs[i], changeKindRegVars) {
				missingAssignment = true
				return false
			}
			return true
		}
		return true
	})
	return !missingAssignment
}
