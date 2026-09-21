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

package swfeatstest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/swfeats"
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
// not contain any child ensure methods. It also checks that each trace log
// corresponds to a registered ensure feature.
func CheckEnsureLoopLogging(filename string, c *check.C, expectChildEnsureMethods bool, submanagerFiles ...string) {
	logTemplate := `logger.Trace("ensure", "manager", "%s", "func", "%s")`
	logLine := func(ensureLog []string) string {
		return fmt.Sprintf(logTemplate, ensureLog[0], ensureLog[1])
	}
	var ensureLogs [][]string
	parsedMgrFile, err := newParsedFile(filename)
	c.Assert(err, check.IsNil)
	childEnsures := ensureCallList(parsedMgrFile.file, "Ensure", childEnsureFunc)
	if expectChildEnsureMethods {
		c.Assert(childEnsures, check.Not(check.HasLen), 0)
	} else {
		c.Assert(childEnsures, check.HasLen, 0)
		return
	}
	ensureReceiver, ok := parsedMgrFile.ensureReceiver()
	c.Assert(ok, check.Equals, true)
	ensureLogs = append(ensureLogs, checkFunctions(parsedMgrFile, ensureReceiver, c, logLine, func(mgr, fun string) []string { return []string{mgr, fun} }, childEnsures...)...)

	submanagerCalls := ensureCallList(parsedMgrFile.file, "Ensure", subManagerCall)
	c.Assert(submanagerFiles, check.HasLen, len(submanagerCalls), check.Commentf(
		"In the Ensure method, the number of submanager calls (%v) does not match the number of provided submanager files (%v). "+
			"Did you add a new submanager in the Ensure method and not yet append its containing file to this function call?",
		len(submanagerCalls), len(submanagerFiles),
	))
	foundCalls := map[submanagerCall]struct{}{}
	for _, file := range submanagerFiles {
		subParsedFile, err := newParsedFile(file)
		c.Assert(err, check.IsNil)
		call, ok := subParsedFile.subEnsure(submanagerCalls)
		c.Assert(ok, check.Equals, true)
		foundCalls[call] = struct{}{}
		subreceiver, subEnsureMethod := call.receiver, call.method
		createSubmanagerLog := func(_ string, function string) []string {
			return []string{ensureReceiver, fmt.Sprintf("%s.%s", subreceiver, function)}
		}
		leftovers := subParsedFile.checkFunctionsForLog(c, func(mgr, function string) string {
			return logLine(createSubmanagerLog(mgr, function))
		}, subEnsureMethod)
		c.Assert(leftovers, check.HasLen, 0)
		ensureLogs = append(ensureLogs, createSubmanagerLog(subreceiver, subEnsureMethod))
		subChildEnsures := ensureCallList(subParsedFile.file, subEnsureMethod, childEnsureFunc)
		ensureLogs = append(ensureLogs, checkFunctions(subParsedFile, ensureReceiver, c, logLine, createSubmanagerLog, subChildEnsures...)...)

	}
	c.Assert(foundCalls, check.HasLen, len(submanagerCalls))

	knownEnsures := make(map[swfeats.EnsureEntry]bool)
	for _, entry := range swfeats.KnownEnsures() {
		knownEnsures[entry] = true
	}
	for _, ensureLog := range ensureLogs {
		entry := swfeats.EnsureEntry{Manager: ensureLog[0], Function: ensureLog[1]}
		c.Check(knownEnsures[entry], check.Equals, true, check.Commentf("ensure trace %q is not registered", entry))
	}
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

func (p *parsedFile) subEnsure(calls []submanagerCall) (submanagerCall, bool) {
	for _, decl := range p.file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		mgr, ok := receiver(funcDecl)
		if !ok {
			continue
		}
		for _, call := range calls {
			if call.receiver == mgr && call.method == funcDecl.Name.Name {
				return call, true
			}
		}
	}
	return submanagerCall{}, false
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

type submanagerCall struct {
	receiver string
	method   string
}

func subManagerCall(callExpr *ast.CallExpr) (submanagerCall, bool) {
	if selectorExpr, ok := callExpr.Fun.(*ast.SelectorExpr); ok {
		functionName := selectorExpr.Sel.Name
		if !strings.HasPrefix(functionName, "Ensure") {
			return submanagerCall{}, false
		}
		for {
			if nextSelector, ok := selectorExpr.X.(*ast.SelectorExpr); ok {
				selectorExpr = nextSelector
			} else {
				break
			}
		}
		if xIdent := selectorExpr.Sel; xIdent != nil {
			return submanagerCall{receiver: xIdent.Name, method: functionName}, true
		}
	}
	return submanagerCall{}, false
}

func ensureCallList[T any](file *ast.File, ensureMethod string, addFunc func(*ast.CallExpr) (T, bool)) []T {
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Name.Name != ensureMethod {
			continue
		}
		var ensures []T
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

func checkFunctions(fileWithEnsure parsedFile, receiver string, c *check.C, logLine func([]string) string, createEnsureLog func(string, string) []string, functions ...string) [][]string {
	createLogLine := func(receiver, function string) string {
		return logLine(createEnsureLog(receiver, function))
	}
	leftovers := fileWithEnsure.checkFunctionsForLog(c, createLogLine, functions...)
	for _, function := range leftovers {
		file, err := fileWithFunction(receiver, function)
		c.Assert(err, check.IsNil)
		parsed, err := newParsedFile(file)
		c.Assert(err, check.IsNil)
		left := parsed.checkFunctionsForLog(c, createLogLine, function)
		c.Assert(left, check.HasLen, 0, check.Commentf("logline %s not found in file %s in function %s", createLogLine(receiver, function), file, function))
	}
	ensureLogs := make([][]string, 0, len(functions))
	for _, function := range functions {
		ensureLogs = append(ensureLogs, createEnsureLog(receiver, function))
	}
	return ensureLogs
}

func fileWithFunction(receiverType, function string) (string, error) {
	items, err := os.ReadDir(".")
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".go") || strings.HasSuffix(item.Name(), "_test.go") {
			continue
		}
		parsed, err := newParsedFile(item.Name())
		if err != nil {
			return "", err
		}
		for _, decl := range parsed.file.Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if !ok || funcDecl.Name.Name != function {
				continue
			}
			if recv, ok := receiver(funcDecl); ok && recv == receiverType {
				return item.Name(), nil
			}
		}
	}
	return "", fmt.Errorf("function %s with receiver %s not found in package", function, receiverType)
}
