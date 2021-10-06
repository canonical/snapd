package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jessevdk/go-flags"
)

type msgID struct {
	msgidPlural string
	comment     string
	fname       string
	line        int
	formatHint  string
}

var msgIDs map[string][]msgID

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

func constructValue(val interface{}) string {
	switch val.(type) {
	case *ast.BasicLit:
		return val.(*ast.BasicLit).Value
	// this happens for constructs like:
	//  gettext.Gettext("foo" + "bar")
	case *ast.BinaryExpr:
		// we only support string concat
		if val.(*ast.BinaryExpr).Op != token.ADD {
			return ""
		}
		left := constructValue(val.(*ast.BinaryExpr).X)
		// strip right " (or `)
		left = left[0 : len(left)-1]
		right := constructValue(val.(*ast.BinaryExpr).Y)
		// strip left " (or `)
		right = right[1:]
		return left + right
	default:
		panic(fmt.Sprintf("unknown type: %v", val))
	}
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
			if sel.Sel.Name == gettextFuncNamePlural && sel.X.(*ast.Ident).Name == gettextSelectorPlural {
				i18nStr = x.Args[0].(*ast.BasicLit).Value
				i18nStrPlural = x.Args[1].(*ast.BasicLit).Value
			}

			if sel.Sel.Name == gettextFuncName && sel.X.(*ast.Ident).Name == gettextSelector {
				i18nStr = constructValue(x.Args[0])
			}

			formatI18nStr := func(s string) string {
				if s == "" {
					return ""
				}
				// the "`" is special
				if s[0] == '`' {
					// keep escaped ", replace inner " with \", replace \n with \\n
					rep := strings.NewReplacer(`\"`, `\"`, `"`, `\"`, "\n", "\\n")
					s = rep.Replace(s)
				}
				// strip leading and trailing " (or `)
				s = s[1 : len(s)-1]
				return s
			}

			// FIXME: too simplistic(?), no %% is considered
			formatHint := ""
			if strings.Contains(i18nStr, "%") || strings.Contains(i18nStrPlural, "%") {
				// well, not quite correct but close enough
				formatHint = "c-format"
			}

			if i18nStr != "" {
				msgidStr := formatI18nStr(i18nStr)
				posCall := fset.Position(n.Pos())
				msgIDs[msgidStr] = append(msgIDs[msgidStr], msgID{
					formatHint:  formatHint,
					msgidPlural: formatI18nStr(i18nStrPlural),
					fname:       posCall.Filename,
					line:        posCall.Line,
					comment:     findCommentsForTranslation(fset, f, posCall),
				})
			}
		}
	}

	return true
}

func processFiles(args []string) error {
	// go over the input files
	msgIDs = make(map[string][]msgID)

	fset := token.NewFileSet()
	for _, fname := range args {
		if err := processSingleGoSource(fset, fname); err != nil {
			return err
		}
	}

	return nil
}

func readContent(fname string) (content []byte, err error) {
	// If no search directories have been specified or we have an
	// absolute path, just try to read the contents directly.
	if len(opts.Directories) == 0 || filepath.IsAbs(fname) {
		return ioutil.ReadFile(fname)
	}

	// Otherwise, search for the file in each of the configured
	// directories.
	for _, dir := range opts.Directories {
		content, err = ioutil.ReadFile(filepath.Join(dir, fname))
		if !os.IsNotExist(err) {
			break
		}
	}
	return content, err
}

func processSingleGoSource(fset *token.FileSet, fname string) error {
	fnameContent, err := readContent(fname)
	if err != nil {
		return err
	}

	// Create the AST by parsing src.
	f, err := parser.ParseFile(fset, fname, fnameContent, parser.ParseComments)
	if err != nil {
		return err
	}

	ast.Inspect(f, func(n ast.Node) bool {
		return inspectNodeForTranslations(fset, f, n)
	})

	return nil
}

var formatTime = func() string {
	return time.Now().Format("2006-01-02 15:04-0700")
}

// mustFprintf will write the given format string to the given
// writer. Any error will make it panic.
func mustFprintf(w io.Writer, format string, a ...interface{}) {
	_, err := fmt.Fprintf(w, format, a...)
	if err != nil {
		panic(fmt.Sprintf("cannot write output: %v", err))
	}
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

`, opts.PackageName, opts.MsgIDBugsAddress, formatTime())
	mustFprintf(out, "%s", header)

	// yes, this is the way to do it in go
	sortedKeys := []string{}
	for k := range msgIDs {
		sortedKeys = append(sortedKeys, k)
	}
	if opts.SortOutput {
		sort.Strings(sortedKeys)
	}

	// FIXME: use template here?
	for _, k := range sortedKeys {
		msgidList := msgIDs[k]
		for _, msgid := range msgidList {
			if opts.AddComments || opts.AddCommentsTag != "" {
				mustFprintf(out, "%s", msgid.comment)
			}
		}
		if !opts.NoLocation {
			mustFprintf(out, "#:")
			for _, msgid := range msgidList {
				mustFprintf(out, " %s:%d", msgid.fname, msgid.line)
			}
			mustFprintf(out, "\n")
		}
		msgid := msgidList[0]
		if msgid.formatHint != "" {
			mustFprintf(out, "#, %s\n", msgid.formatHint)
		}
		var formatOutput = func(in string) string {
			// split string with \n into multiple lines
			// to make the output nicer
			out := strings.Replace(in, "\\n", "\\n\"\n        \"", -1)
			// cleanup too aggressive splitting (empty "" lines)
			return strings.TrimSuffix(out, "\"\n        \"")
		}
		mustFprintf(out, "msgid   \"%v\"\n", formatOutput(k))
		if msgid.msgidPlural != "" {
			mustFprintf(out, "msgid_plural   \"%v\"\n", formatOutput(msgid.msgidPlural))
			mustFprintf(out, "msgstr[0]  \"\"\n")
			mustFprintf(out, "msgstr[1]  \"\"\n")
		} else {
			mustFprintf(out, "msgstr  \"\"\n")
		}
		mustFprintf(out, "\n")
	}

}

// FIXME: this must be setable via go-flags
var opts struct {
	FilesFrom string `short:"f" long:"files-from" description:"get list of input files from FILE"`

	Directories []string `short:"D" long:"directory" description:"add DIRECTORY to list for input files search"`

	Output string `short:"o" long:"output" description:"output to specified file"`

	AddComments bool `short:"c" long:"add-comments" description:"place all comment blocks preceding keyword lines in output file"`

	AddCommentsTag string `long:"add-comments-tag" description:"place comment blocks starting with TAG and prceding keyword lines in output file"`

	SortOutput bool `short:"s" long:"sort-output" description:"generate sorted output"`

	NoLocation bool `long:"no-location" description:"do not write '#: filename:line' lines"`

	MsgIDBugsAddress string `long:"msgid-bugs-address" default:"EMAIL" description:"set report address for msgid bugs"`

	PackageName string `long:"package-name" description:"set package name in output"`

	Keyword       string `short:"k" long:"keyword" default:"gettext.Gettext" description:"look for WORD as the keyword for singular strings"`
	KeywordPlural string `long:"keyword-plural" default:"gettext.NGettext" description:"look for WORD as the keyword for plural strings"`
}

func main() {
	// parse args
	args, err := flags.ParseArgs(&opts, os.Args)
	if err != nil {
		log.Fatalf("ParseArgs failed %s", err)
	}

	var files []string
	if opts.FilesFrom != "" {
		content, err := ioutil.ReadFile(opts.FilesFrom)
		if err != nil {
			log.Fatalf("cannot read file %v: %v", opts.FilesFrom, err)
		}
		content = bytes.TrimSpace(content)
		files = strings.Split(string(content), "\n")
	} else {
		files = args[1:]
	}
	if err := processFiles(files); err != nil {
		log.Fatalf("processFiles failed with: %s", err)
	}

	out := os.Stdout
	if opts.Output != "" {
		var err error
		out, err = os.Create(opts.Output)
		if err != nil {
			log.Fatalf("failed to create %s: %s", opts.Output, err)
		}
	}
	writePotFile(out)
}
