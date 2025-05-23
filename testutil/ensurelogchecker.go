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
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/strutil"
)

// CheckEnsureLoopLogging checks for trace log statement coverage in
// StateManager implementers.
// The provided file (*mgr.go) should contain a StateManager Ensure implementation.
// If the specified file uses submanagers, their source files must be included.
// If expectChildEnsureMethods, will check that the Ensure method in the go
// source code, indicated by the given file name, has at least one child
// ensure method and the file contains at least one trace log inside each
// ensure* method called within that file's Ensure() method.
// If not expectChildEnsureMethods, then the go source code must
// not contain any child ensure methods.
func CheckEnsureLoopLogging(filename string, c *check.C, expectChildEnsureMethods bool, submanagerFiles ...string) {
	logTemplate := `logger.Trace("ensure", "manager", "%s", "func", "%s")`
	parsedMgrFile, err := newParsedFile(filename)
	c.Assert(err, check.IsNil)
	childEnsures := ensureCallList(parsedMgrFile.file, childEnsureFunc)
	if expectChildEnsureMethods {
		c.Assert(childEnsures, check.Not(check.HasLen), 0)
	} else {
		c.Assert(childEnsures, check.HasLen, 0)
		return
	}
	ensureReceiver, ok := parsedMgrFile.ensureReceiver()
	c.Assert(ok, check.Equals, true)
	checkFunctions(parsedMgrFile, ensureReceiver, c, func(mgr, fun string) string { return fmt.Sprintf(logTemplate, mgr, fun) }, childEnsures...)

	submanagerCalls := ensureCallList(parsedMgrFile.file, subManagerFunc)
	c.Assert(submanagerFiles, check.HasLen, len(submanagerCalls), check.Commentf(
		"In the Ensure method, the number of submanager calls (%v) does not match the number of provided submanager files (%v). "+
			"Did you add a new submanager in the Ensure method and not yet append its containing file to this function call?",
		len(submanagerCalls), len(submanagerFiles),
	))
	foundCalls := map[string]struct{}{}
	for _, file := range submanagerFiles {
		subParsedFile, err := newParsedFile(file)
		c.Assert(err, check.IsNil)
		subreceiver, ok := subParsedFile.ensureReceiver()
		c.Assert(ok, check.Equals, true)
		c.Assert(strutil.ListContains(submanagerCalls, subreceiver), check.Equals, true)
		foundCalls[subreceiver] = struct{}{}
		leftovers := subParsedFile.checkFunctionsForLog(c, func(mgr, _ string) string {
			return fmt.Sprintf(logTemplate, ensureReceiver, fmt.Sprintf("%s.Ensure", mgr))
		}, "Ensure")
		c.Assert(leftovers, check.HasLen, 0)
		subChildEnsures := ensureCallList(subParsedFile.file, childEnsureFunc)
		checkFunctions(subParsedFile, ensureReceiver, c, func(mgr, fun string) string {
			return fmt.Sprintf(logTemplate, ensureReceiver, fmt.Sprintf("%s.%s", mgr, fun))
		}, subChildEnsures...)

	}
	c.Assert(foundCalls, check.HasLen, len(submanagerCalls))
}

type parsedFile struct {
	filename    string
	fset        *token.FileSet
	fileContent string
	file        *ast.File
}

func newParsedFile(filename string) (parsedFile, error) {
	fset := token.NewFileSet()
	content, err := os.ReadFile(filename)
	if err != nil {
		return parsedFile{}, err
	}
	fileContent := string(content)
	file, err := parser.ParseFile(fset, filename, fileContent, parser.AllErrors)
	if err != nil {
		return parsedFile{}, err
	}
	return parsedFile{filename: filename, fset: fset, fileContent: fileContent, file: file}, nil
}

func (p *parsedFile) ensureReceiver() (string, bool) {
	for _, decl := range p.file.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			if funcDecl.Name.Name != "Ensure" {
				continue
			}
			if mgr, ok := receiver(funcDecl); ok {
				return mgr, true
			}
		}
	}
	return "", false
}

func (p *parsedFile) checkFunctionsForLog(c *check.C, createLogLine func(string, string) string, functions ...string) []string {
	checked := []string{}
	for _, decl := range p.file.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			mgr, ok := receiver(funcDecl)
			if !ok || !strutil.ListContains(functions, funcDecl.Name.Name) {
				continue
			}
			checked = append(checked, funcDecl.Name.Name)
			expected := createLogLine(mgr, funcDecl.Name.Name)
			foundTraceLog := p.bodyContainsString(funcDecl.Body, expected)
			c.Assert(foundTraceLog, check.Equals, true, check.Commentf("In file %s in function %s, the following trace log was not found: %s", p.filename, funcDecl.Name.Name, expected))
		}
	}
	difference := []string{}
	for _, item := range functions {
		if !strutil.ListContains(checked, item) {
			difference = append(difference, item)
		}
	}
	return difference
}

func (p *parsedFile) bodyContainsString(block *ast.BlockStmt, expected string) bool {
	for _, stmt := range block.List {
		if ifStmt, ok := stmt.(*ast.IfStmt); ok {
			if p.bodyContainsString(ifStmt.Body, expected) {
				return true
			}
			if elseStmt, ok := ifStmt.Else.(*ast.BlockStmt); ok {
				if p.bodyContainsString(elseStmt, expected) {
					return true
				}
			}
		} else if exprStmt, ok := stmt.(*ast.ExprStmt); ok {
			start := p.fset.Position(exprStmt.X.Pos()).Offset
			end := p.fset.Position(exprStmt.X.End()).Offset
			stringed := p.fileContent[start:end]
			if expected == stringed {
				return true
			}
		}
	}
	return false
}

func receiver(funcDecl *ast.FuncDecl) (string, bool) {
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

func childEnsureFunc(callExpr *ast.CallExpr) (string, bool) {
	if selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
		functionName := selectorExpr.Sel.Name
		if strings.HasPrefix(functionName, "ensure") {
			return functionName, true
		}
	}
	return "", false
}

func subManagerFunc(callExpr *ast.CallExpr) (string, bool) {
	if selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
		functionName := selectorExpr.Sel.Name
		if functionName != "Ensure" {
			return "", false
		}
		for {
			if nextSelector, ok := selectorExpr.X.(*ast.SelectorExpr); ok {
				selectorExpr = nextSelector
			} else {
				break
			}
		}
		if xIdent := selectorExpr.Sel; xIdent != nil {
			return xIdent.Name, true
		}
	}
	return "", false
}

func ensureCallList(file *ast.File, addFunc func(*ast.CallExpr) (string, bool)) []string {
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name.Name != "Ensure" {
			continue
		}
		var ensures []string
		ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
			if callExpr, ok := n.(*ast.CallExpr); ok {
				if name, ok := addFunc(callExpr); ok {
					ensures = append(ensures, name)
				}
			}
			return true
		})
		return ensures
	}
	return nil
}

func checkFunctions(fileWithEnsure parsedFile, receiver string, c *check.C, createLogLine func(string, string) string, functions ...string) {
	leftovers := fileWithEnsure.checkFunctionsForLog(c, createLogLine, functions...)
	for _, function := range leftovers {
		file, err := fileWithFunction(receiver, function)
		c.Assert(err, check.IsNil)
		parsed, err := newParsedFile(file)
		c.Assert(err, check.IsNil)
		left := parsed.checkFunctionsForLog(c, createLogLine, function)
		c.Assert(left, check.HasLen, 0, check.Commentf("logline %s not found in file %s in function %s", createLogLine(receiver, function), file, function))
	}
}

func fileWithFunction(receiver, function string) (string, error) {
	pattern := fmt.Sprintf("*%s) %s(", receiver, function)
	items, err := os.ReadDir(".")
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".go") || strings.HasSuffix(item.Name(), "_test.go") {
			continue
		}
		file, err := os.Open(item.Name())
		if err != nil {
			return "", err
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), pattern) {
				return item.Name(), nil
			}
		}
	}
	return "", fmt.Errorf("function %s with receiver %s not found in package", function, receiver)
}
