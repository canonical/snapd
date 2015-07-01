package main

import (
	"fmt"
	"io/ioutil"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
)

var gettextSelector = "i18n"
var gettextFuncName = "G"

const header = `
# SOME DESCRIPTIVE TITLE.
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

func main() {
	fname := os.Args[1]
	fnameContent, err := ioutil.ReadFile(fname)
	if err != nil {
		panic(err)
	}
	
	// Create the AST by parsing src.
	fset := token.NewFileSet() // positions are relative to fset
	f, err := parser.ParseFile(fset, fname, fnameContent, parser.ParseComments)
	if err != nil {
		panic(err)
	}

	fmt.Println(header)
	
	ast.Inspect(f, func(n ast.Node) bool {
		var i18nStr string
		switch x := n.(type) {
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == gettextFuncName && sel.X.(*ast.Ident).Name == gettextSelector {
					i18nStr = x.Args[0].(*ast.BasicLit).Value
					// strip " (or `)
					i18nStr = i18nStr[1:len(i18nStr)-1]
					posCall := fset.Position(n.Pos())
					com := ""
					for _, cg := range(f.Comments) {
						for i := len(cg.List)-1; i >= 0; i-- {
							c := cg.List[i]
							posComment := fset.Position(c.Slash)
							//println(posCall.Line, posComment.Line, c.Text)
							if posCall.Line == posComment.Line+1 {
								posCall = posComment
								com = fmt.Sprintf("#. %s\n%s", c.Text, com)
							}
						}
					}
					fmt.Printf("%s", com)
					fmt.Printf("msgid \"%v\"\n", strings.Replace(i18nStr, "\n", "\\n", -1))
					fmt.Printf("msgstr \"\"\n\n")
				}
			}
		}
		return true
	})

}
