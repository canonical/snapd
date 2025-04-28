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

package testutil

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

func getReceiver(funcDecl *ast.FuncDecl) (string, bool) {
	if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
		if starExpr, ok := funcDecl.Recv.List[0].Type.(*ast.StarExpr); ok {
			if ident, ok := starExpr.X.(*ast.Ident); ok {
				return ident.Name, true
			}
		} else if ident, ok := funcDecl.Recv.List[0].Type.(*ast.Ident); ok {
			return ident.Name, true
		}
	}
	return "", false
}

func getChildEnsureList(file *ast.File) []string {
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name.Name != "Ensure" {
			continue
		}
		var ensures []string
		ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
			if callExpr, ok := n.(*ast.CallExpr); ok {
				if selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
					functionName := selectorExpr.Sel.Name
					if strings.HasPrefix(functionName, "ensure") {
						ensures = append(ensures, functionName)
					}
				}
			}
			return true
		})
		return ensures
	}
	return nil
}

func checkBodyForString(fset *token.FileSet, fileContent string, block *ast.BlockStmt, expected string) bool {
	for _, stmt := range block.List {
		if ifStmt, ok := stmt.(*ast.IfStmt); ok {
			if checkBodyForString(fset, fileContent, ifStmt.Body, expected) {
				return true
			}
			if elseStmt, ok := ifStmt.Else.(*ast.BlockStmt); ok {
				if checkBodyForString(fset, fileContent, elseStmt, expected) {
					return true
				}
			}
		} else if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
			start := fset.Position(exprStmt.X.Pos()).Offset
			end := fset.Position(exprStmt.X.End()).Offset
			stringed := fileContent[start:end]
			if expected == stringed {
				return true
			}
		}
	}
	return false
}

// if expectChildEnsureMethods, checks that the Ensure method in the go
// source code, indicated by the given file name, has at least one child
// ensure method and the file contains at least one trace log inside each
// ensure* method called within that file's Ensure() method.
// if not expectChildEnsureMethods, then the go source code must
// not contain any child ensure methods.
func CheckEnsureLoopLogging(filename string, c *check.C, expectChildEnsureMethods bool) {
	fset := token.NewFileSet()
	content, err := os.ReadFile(filename)
	c.Assert(err, check.IsNil)
	fileContent := string(content)
	file, err := parser.ParseFile(fset, filename, fileContent, parser.AllErrors)
	c.Assert(err, check.IsNil)
	childEnsures := getChildEnsureList(file)
	if expectChildEnsureMethods {
		c.Assert(len(childEnsures), IntGreaterThan, 0)
	} else {
		c.Assert(len(childEnsures), IntEqual, 0)
		return
	}
	for _, decl := range file.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			mgr, ok := getReceiver(funcDecl)
			if !ok || !strutil.ListContains(childEnsures, funcDecl.Name.Name) {
				continue
			}
			expected := fmt.Sprintf(`logger.Trace("ensure", "manager", "%s", "func", "%s")`, mgr, funcDecl.Name.Name)
			foundTraceLog := checkBodyForString(fset, fileContent, funcDecl.Body, expected)
			c.Assert(foundTraceLog, check.Equals, true, check.Commentf("In file %s in function %s, the following trace log was not found: %s", filename, funcDecl.Name.Name, expected))
		}
	}
}

// within a given folder, finds all files that contain an
// implementation of the StateManager interface
func GetListOfStateManagerImplementers(folder string) ([]string, error) {
	pattern := ") Ensure()"

	filepaths := []string{}
	err := filepath.Walk(folder, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.Contains(path, "_test.go") {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), pattern) {
				filepaths = append(filepaths, path)
				break
			}
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return filepaths, nil
}
