package main

import (
	"fmt"
	"io"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"strings"
)

var gettextSelector = "i18n"
var gettextFuncName = "G"

const header = `# SOME DESCRIPTIVE TITLE.
# Copyright (C) YEAR THE PACKAGE'S COPYRIGHT HOLDER
# This file is distributed under the same license as the PACKAGE package.
# FIRST AUTHOR <EMAIL@ADDRESS>, YEAR.
#
#, fuzzy
msgid   ""
msgstr  "Project-Id-Version: snappy\n"
        "Report-Msgid-Bugs-To: snappy-devel@lists.ubuntu.com\n"
        "POT-Creation-Date: 2015-06-30 14:48+0200\n"
        "PO-Revision-Date: YEAR-MO-DA HO:MI+ZONE\n"
        "Last-Translator: FULL NAME <EMAIL@ADDRESS>\n"
        "Language-Team: LANGUAGE <LL@li.org>\n"
        "Language: \n"
        "MIME-Version: 1.0\n"
        "Content-Type: text/plain; charset=CHARSET\n"
        "Content-Transfer-Encoding: 8bit\n"

`

func formatComment(com string) string {
	out := ""
	for _, rawline := range strings.Split(com, "\n") {
		line := rawline
		line = strings.TrimPrefix(line, "//")
		line = strings.TrimPrefix(line, "/*")
		line = strings.TrimSuffix(line, "*/")
		line = strings.TrimSpace(line)
		if line != "" {
			out += fmt.Sprintf("#. %s\n", line)
		}
	}

	return out
}

func findCommentsForTranslation(fset *token.FileSet, f *ast.File, posCall token.Position) string {
	com := ""
	for _, cg := range f.Comments {
		// search for all comments in the previous line
		for i := len(cg.List) - 1; i >= 0; i-- {
			c := cg.List[i]

			posComment := fset.Position(c.End())
			//println(posCall.Line, posComment.Line, c.Text)
			if posCall.Line == posComment.Line+1 {
				posCall = posComment
				com = fmt.Sprintf("%s\n%s", c.Text, com)
			}
		}
	}
	return formatComment(com)
}

func inspectNodeForTranslations(fset *token.FileSet, f *ast.File, n ast.Node, out io.Writer) bool {
	switch x := n.(type) {
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			if sel.Sel.Name == gettextFuncName && sel.X.(*ast.Ident).Name == gettextSelector {
				i18nStr := x.Args[0].(*ast.BasicLit).Value
				// strip " (or `)
				i18nStr = i18nStr[1 : len(i18nStr)-1]
				posCall := fset.Position(n.Pos())

				fmt.Fprintf(out, "#: %s:%d\n", posCall.Filename, posCall.Line)
				fmt.Fprintf(out, "%s", findCommentsForTranslation(fset, f, posCall))
				fmt.Fprintf(out, "msgid \"%v\"\n", strings.Replace(i18nStr, "\n", "\\n", -1))
				fmt.Fprintf(out, "msgstr \"\"\n\n")
			}
		}
	}

	return true
}

func processSingleGoSource(fset *token.FileSet, fname string, out io.Writer) {
	fnameContent, err := ioutil.ReadFile(fname)
	if err != nil {
		panic(err)
	}

	// Create the AST by parsing src.
	f, err := parser.ParseFile(fset, fname, fnameContent, parser.ParseComments)
	if err != nil {
		panic(err)
	}

	ast.Inspect(f, func(n ast.Node) bool {
		return inspectNodeForTranslations(fset, f, n, out)
	})

}

func main() {
	fmt.Println(header)

	fset := token.NewFileSet() // positions are relative to fset
	for _, fname := range os.Args[1:] {
		processSingleGoSource(fset, fname, os.Stdout)
	}
}
