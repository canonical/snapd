package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
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
	wd, _ = filepath.Abs(wd)

	var excludedDirNames = map[string]bool{
		"vendor": true,
		".git":   true,
		"tests":  true,
	}

	autoExcludedDirs, err := loggerDependencyDirs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not calculate logger dependencies: %v\n", err)
		os.Exit(1)
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "skipping %s: %v\n", path, err)
			os.Exit(1)
		}
		if d.IsDir() && shouldSkipDir(path, d.Name(), excludedDirNames, autoExcludedDirs) {
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

func shouldSkipDir(path, dirName string, excludedDirNames, excludedAbsDirs map[string]bool) bool {
	if excludedDirNames[dirName] {
		return true
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	for ex := range excludedAbsDirs {
		if absPath == ex || strings.HasPrefix(absPath, ex+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

func loggerDependencyDirs() (map[string]bool, error) {
	outDefault, err := runCmdOutput("go", "list", "-e", "-deps", "-f", "{{if and (not .Standard) .Module.Main}}{{.Dir}}{{end}}", "./logger")
	if err != nil {
		return nil, err
	}
	outStructured, err := runCmdOutput("go", "list", "-e", "-tags", "structuredlogging", "-deps", "-f", "{{if and (not .Standard) .Module.Main}}{{.Dir}}{{end}}", "./logger")
	if err != nil {
		return nil, err
	}
	outDefaultTest, err := runCmdOutput("go", "list", "-e", "-test", "-deps", "-f", "{{if and (not .Standard) .Module.Main}}{{.Dir}}{{end}}", "./logger")
	if err != nil {
		return nil, err
	}
	outStructuredTest, err := runCmdOutput("go", "list", "-e", "-test", "-tags", "structuredlogging", "-deps", "-f", "{{if and (not .Standard) .Module.Main}}{{.Dir}}{{end}}", "./logger")
	if err != nil {
		return nil, err
	}

	dirs := map[string]bool{}
	for _, line := range strings.Split(outDefault, "\n") {
		d := strings.TrimSpace(line)
		if d == "" {
			continue
		}
		if abs, err := filepath.Abs(d); err == nil {
			dirs[abs] = true
		}
	}
	for _, line := range strings.Split(outStructured, "\n") {
		d := strings.TrimSpace(line)
		if d == "" {
			continue
		}
		if abs, err := filepath.Abs(d); err == nil {
			dirs[abs] = true
		}
	}
	for _, line := range strings.Split(outDefaultTest, "\n") {
		d := strings.TrimSpace(line)
		if d == "" {
			continue
		}
		if abs, err := filepath.Abs(d); err == nil {
			dirs[abs] = true
		}
	}
	for _, line := range strings.Split(outStructuredTest, "\n") {
		d := strings.TrimSpace(line)
		if d == "" {
			continue
		}
		if abs, err := filepath.Abs(d); err == nil {
			dirs[abs] = true
		}
	}

	return dirs, nil
}

func runCmdOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("%s %s failed: %s", name, strings.Join(args, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("%s %s failed: %v", name, strings.Join(args, " "), err)
	}
	return string(out), nil
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
	var lastImportEnd int

	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		lastImportEnd = fset.Position(genDecl.End()).Offset

		hasC := false
		for _, spec := range genDecl.Specs {
			if ispec, ok := spec.(*ast.ImportSpec); ok && ispec.Path != nil && ispec.Path.Value == `"C"` {
				hasC = true
				break
			}
		}
		if hasC {
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
	if lastImportEnd > 0 {
		newImportBlock := "\nimport (" + importLine + "\n)"
		src = append(src[:lastImportEnd], append([]byte(newImportBlock), src[lastImportEnd:]...)...)
		return src
	}

	return src
}
