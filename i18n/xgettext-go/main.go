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

	"github.com/jessevdk/go-flags"
)

type msgId struct {
	msgid       string
	msgidPlural string
	comment     string
	fname       string
	line        int
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
	needle := fmt.Sprintf("#. %s", opts.AddCommentsTag)
	if !strings.HasPrefix(formatedComment, needle) {
		formatedComment = ""
	}

	return formatedComment
}

func inspectNodeForTranslations(fset *token.FileSet, f *ast.File, n ast.Node) bool {
	// FIXME: this assume we always have a "gettext.Gettext" style keyword
	l := strings.Split(opts.Keyword, ".")
	gettextSelector := l[0]
	gettextFuncName := l[1]

	l = strings.Split(opts.KeywordPlural, ".")
	gettextSelectorPlural := l[0]
	gettextFuncNamePlural := l[1]

	switch x := n.(type) {
	case *ast.CallExpr:
		if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
			i18nStr := ""
			i18nStrPlural := ""
			if sel.X.(*ast.Ident).Name == gettextSelectorPlural && sel.Sel.Name == gettextFuncNamePlural {
				i18nStr = x.Args[0].(*ast.BasicLit).Value
				i18nStrPlural = x.Args[1].(*ast.BasicLit).Value
			}

			if sel.X.(*ast.Ident).Name == gettextSelector && sel.Sel.Name == gettextFuncName {
				i18nStr = x.Args[0].(*ast.BasicLit).Value
			}

			formatI18nStr := func(s string) string {
				if s == "" {
					return ""
				}
				// strip " (or `)
				s = s[1 : len(s)-1]
				return strings.Replace(s, "\n", "\\n", -1)
			}

			if i18nStr != "" {
				msgidStr := formatI18nStr(i18nStr)
				posCall := fset.Position(n.Pos())
				msgIds[msgidStr] = msgId{
					msgid:       msgidStr,
					msgidPlural: formatI18nStr(i18nStrPlural),
					fname:       posCall.Filename,
					line:        posCall.Line,
					comment:     findCommentsForTranslation(fset, f, posCall),
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

`, opts.PackageName, opts.MsgIdBugsAddress, formatTime())
	fmt.Fprintf(out, "%s", header)

	// yes, this is the way to do it in go
	sortedKeys := []string{}
	for k := range msgIds {
		sortedKeys = append(sortedKeys, k)
	}
	if opts.SortOutput {
		sort.Strings(sortedKeys)
	}

	// output sorted
	for _, k := range sortedKeys {
		msgid := msgIds[k]
		if !opts.NoLocation {
			fmt.Fprintf(out, "#: %s:%d\n", msgid.fname, msgid.line)
		}
		if opts.AddComments || opts.AddCommentsTag != "" {
			fmt.Fprintf(out, "%s", msgid.comment)
		}
		fmt.Fprintf(out, "msgid   \"%v\"\n", msgid.msgid)
		if msgid.msgidPlural != "" {
			fmt.Fprintf(out, "msgid_plural   \"%v\"\n", msgid.msgidPlural)
			fmt.Fprintf(out, "msgstr[0]  \"\"\n")
			fmt.Fprintf(out, "msgstr[1]  \"\"\n")
		} else {
			fmt.Fprintf(out, "msgstr  \"\"\n")
		}
		fmt.Fprintf(out, "\n")
	}

}

// FIXME: this must be setable via go-flags
var opts struct {
	Output string `short:"o" long:"output" description:"output to specified file"`

	AddComments bool `short:"c" long:"add-comments" description:"place all comment blocks preceding keyword lines in output file"`

	AddCommentsTag string `long:"add-comments-tag" description:"place comment blocks starting with TAG and prceding keyword lines in output file"`

	SortOutput bool `short:"s" long:"sort-output" description:"generate sorted output"`

	NoLocation bool `long:"no-location" description:"do not write '#: filename:line' lines"`

	MsgIdBugsAddress string `long:"msgid-bugs-address" default:"EMAIL" description:"set report address for msgid bugs"`

	PackageName string `long:"package-name" description:"set package name in output"`

	Keyword       string `short:"k" long:"keyword" default:"gettext.Gettext" description:"look for WORD as the keyword for singular strings"`
	KeywordPlural string `long:"keyword-plural" default:"gettext.NGettext" description:"look for WORD as the keyword for plural strings"`
}

func main() {
	// parse args
	args, err := flags.ParseArgs(&opts, os.Args)
	if err != nil {
		fmt.Errorf("ParseArgs failed %s", err)
	}

	// go over the input files
	fset := token.NewFileSet()
	for _, fname := range args[1:] {
		processSingleGoSource(fset, fname)
	}

	out := os.Stdout
	if opts.Output != "" {
		var err error
		out, err = os.Create(opts.Output)
		if err != nil {
			fmt.Errorf("failed to create %s", opts.Output, err)
		}
	}
	writePotFile(out)
}
