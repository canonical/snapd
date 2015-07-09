package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"time"
)

// FIXME: this must be setable via go-flags
var gettextSelector = "i18n"
var gettextFuncName = "G"
var sortMsgIds = true
var showLocation = false
var projectName = "snappy"
var projectMsgIdBugs = "snappy-devel@lists.ubuntu.com"
var commentsTag = "TRANSLATORS:"
var output = ""

type msgId struct {
	msgid   string
	comment string
	fname   string
	line    int
}

var msgIds = make(map[string]msgId)

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

	// only return if we have a matching prefix
	formatedComment := formatComment(com)
	needle := fmt.Sprintf("#. %s", commentsTag)
	if !strings.HasPrefix(formatedComment, needle) {
		formatedComment = ""
	}

	return formatedComment
}

func inspectNodeForTranslations(fset *token.FileSet, f *ast.File, n ast.Node) bool {
	switch x := n.(type) {
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			if sel.Sel.Name == gettextFuncName && sel.X.(*ast.Ident).Name == gettextSelector {
				i18nStr := x.Args[0].(*ast.BasicLit).Value
				// strip " (or `)
				i18nStr = i18nStr[1 : len(i18nStr)-1]
				posCall := fset.Position(n.Pos())
				msgidStr := strings.Replace(i18nStr, "\n", "\\n", -1)
				msgIds[msgidStr] = msgId{
					msgid:   msgidStr,
					fname:   posCall.Filename,
					line:    posCall.Line,
					comment: findCommentsForTranslation(fset, f, posCall),
				}
			}
		}
	}

	return true
}

func processSingleGoSource(fset *token.FileSet, fname string) {
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
		return inspectNodeForTranslations(fset, f, n)
	})

}

var formatTime = func() string {
	return time.Now().Format("2006-01-02 15:04-0700")
}

func writePotFile(out io.Writer) {

	header := fmt.Sprintf(`# SOME DESCRIPTIVE TITLE.
# Copyright (C) YEAR THE PACKAGE'S COPYRIGHT HOLDER
# This file is distributed under the same license as the PACKAGE package.
# FIRST AUTHOR <EMAIL@ADDRESS>, YEAR.
#
#, fuzzy
msgid   ""
msgstr  "Project-Id-Version: %s\n"
        "Report-Msgid-Bugs-To: %s\n"
        "POT-Creation-Date: %s\n"
        "PO-Revision-Date: YEAR-MO-DA HO:MI+ZONE\n"
        "Last-Translator: FULL NAME <EMAIL@ADDRESS>\n"
        "Language-Team: LANGUAGE <LL@li.org>\n"
        "Language: \n"
        "MIME-Version: 1.0\n"
        "Content-Type: text/plain; charset=CHARSET\n"
        "Content-Transfer-Encoding: 8bit\n"

`, projectName, projectMsgIdBugs, formatTime())
	fmt.Fprintf(out, "%s", header)

	// yes, this is the way to do it in go
	sortedKeys := []string{}
	for k := range msgIds {
		sortedKeys = append(sortedKeys, k)
	}
	if sortMsgIds {
		sort.Strings(sortedKeys)
	}

	// output sorted
	for _, k := range sortedKeys {
		msgid := msgIds[k]
		if showLocation {
			fmt.Fprintf(out, "#: %s:%d\n", msgid.fname, msgid.line)
		}
		fmt.Fprintf(out, "%s", msgid.comment)
		fmt.Fprintf(out, "msgid \"%v\"\n", msgid.msgid)
		fmt.Fprintf(out, "msgstr \"\"\n\n")
	}

}

func main() {
	fset := token.NewFileSet() // positions are relative to fset
	for _, fname := range os.Args[1:] {
		processSingleGoSource(fset, fname)
	}

	out := os.Stdout
	if output != "" {
		var err error
		out, err = os.Create(output)
		if err != nil {
			fmt.Errorf("failed to create %s", output, err)
		}
	}
	writePotFile(out)
}
