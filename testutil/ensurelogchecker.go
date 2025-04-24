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
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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

func getChildEnsureList(fset *token.FileSet, fileContent string, file *ast.File) []string {
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name.Name != "Ensure" {
			continue
		}
		ensures := []string{}
		ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
			if callExpr, ok := n.(*ast.CallExpr); ok {
				start := fset.Position(callExpr.Fun.Pos()).Offset
				end := fset.Position(callExpr.Fun.End()).Offset
				if strings.Contains(fileContent[start:end], "ensure") {
					parts := strings.Split(fileContent[start:end], ".")
					if len(parts) > 1 {
						ensures = append(ensures, parts[1])
					} else {
						ensures = append(ensures, fileContent[start:end])
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

func CheckEnsureLoopLogging(filename string, c *check.C) {
	fset := token.NewFileSet()
	content, err := os.ReadFile(filename)
	c.Assert(err, check.IsNil)
	fileContent := string(content)
	file, err := parser.ParseFile(fset, filename, fileContent, parser.AllErrors)
	c.Assert(err, check.IsNil)
	childEnsures := getChildEnsureList(fset, fileContent, file)
	if len(childEnsures) == 0 {
		return
	}
	for _, decl := range file.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			mgr, ok := getReceiver(funcDecl)
			if !ok || !strutil.ListContains(childEnsures, funcDecl.Name.Name) {
				continue
			}
			expected := fmt.Sprintf("logger.Trace(\"ensure\", \"manager\", \"%s\", \"func\", \"%s\")", mgr, funcDecl.Name.Name)
			foundTraceLog := checkBodyForString(fset, fileContent, funcDecl.Body, expected)
			c.Assert(foundTraceLog, check.Equals, true, check.Commentf("In file %s in function %s, the following trace log was not found: %s", filename, funcDecl.Name.Name, expected))
		}
	}
}
