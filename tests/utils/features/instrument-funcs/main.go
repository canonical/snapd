package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	wd, _ := os.Getwd()

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %v\n", path, err)
			os.Exit(1)
		}
		if d.IsDir() && (d.Name() == "vendor" || d.Name() == ".git" ||
			d.Name() == "logger" || d.Name() == "osutil" ||
			d.Name() == "randutil" || d.Name() == "strutil" ||
			d.Name() == "testutil" || d.Name() == "dbusutil" ||
			d.Name() == "tests" || d.Name() == "release" ||
			d.Name() == "blkid" || d.Name() == "snap-seccomp" ||
			d.Name() == "snap-update-ns" || d.Name() == "dirs") {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(path, ".go") || d.IsDir() || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if err := instrumentFile(path, wd); err != nil {
			fmt.Fprintf(os.Stderr, "error: %s: %v\n", path, err)
			os.Exit(1)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk error: %v\n", err)
		os.Exit(1)
	}
}

func instrumentFile(path, wd string) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return fmt.Errorf("parse: %v", err)
	}

	relPath := filepath.ToSlash(path)
	if r, err := filepath.Rel(wd, path); err == nil {
		relPath = filepath.ToSlash(r)
	}

	inLoggerPkg := f.Name.Name == "logger"

	type insertion struct {
		offset int
		text   string
	}
	var insertions []insertion

	for _, decl := range f.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok || funcDecl.Body == nil {
			continue
		}

		lbrace := fset.Position(funcDecl.Body.Lbrace).Offset
		insOffset := lbrace + 1

		bodyOnSameLine := insOffset < len(src) && src[insOffset] != '\n'

		funcName := funcDecl.Name.Name

		insText := fmt.Sprintf("\n\t%s(\"coverage\", \"file\", %s, \"func\", %s)",
			"logger.Trace", strLit(relPath), strLit(funcName))
		if bodyOnSameLine {
			insText += "\n"
		}

		insertions = append(insertions, insertion{insOffset, insText})
	}

	if len(insertions) == 0 {
		return nil
	}

	sort.Slice(insertions, func(i, j int) bool {
		return insertions[i].offset > insertions[j].offset
	})

	for _, ins := range insertions {
		src = append(src[:ins.offset], append([]byte(ins.text), src[ins.offset:]...)...)
	}

	if !inLoggerPkg && !hasLoggerImport(f) {
		src = addImportToSource(src, fset, f, `"github.com/snapcore/snapd/logger"`)
	}

	result, err := format.Source(src)
	if err != nil {
		return fmt.Errorf("format: %v", err)
	}

	return os.WriteFile(path, result, 0644)
}

func strLit(s string) string {
	return fmt.Sprintf("%q", s)
}

func hasLoggerImport(f *ast.File) bool {
	for _, imp := range f.Imports {
		if imp.Path.Value == `"github.com/snapcore/snapd/logger"` {
			return true
		}
	}
	return false
}

func addImportToSource(src []byte, fset *token.FileSet, f *ast.File, importPath string) []byte {
	importLine := "\n\t" + importPath

	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}

		if genDecl.Lparen.IsValid() {
			// Grouped import: `import ( ... )`
			endOffset := fset.Position(genDecl.End()).Offset
			insertAt := endOffset - 1
			src = append(src[:insertAt], append([]byte(importLine+"\n"), src[insertAt:]...)...)
		} else {
			// Single import: `import "..."` — convert to grouped form
			startOffset := fset.Position(genDecl.Pos()).Offset
			endOffset := fset.Position(genDecl.End()).Offset
			existingImport := src[startOffset:endOffset]
			grouped := "import (\n\t" + string(existingImport[len("import "):]) + importLine + "\n)"
			before := make([]byte, startOffset)
			copy(before, src[:startOffset])
			src = append(before, append([]byte(grouped), src[endOffset:]...)...)
		}
		return src
	}

	pkgEnd := fset.Position(f.Name.End()).Offset
	newImportBlock := "\nimport (" + importLine + "\n)\n"
	src = append(src[:pkgEnd], append([]byte(newImportBlock), src[pkgEnd:]...)...)
	return src
}
