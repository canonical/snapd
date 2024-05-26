package main

import (
	"fmt"
	"go/ast"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

const (
	errorKindsHdr      = `## <h3 id='heading--errors'>Error kinds</h3>`
	maintErrorKindsHdr = `## <h4 id='heading--maint-errors'>Maintenance error kinds</h4>

These are used only inside the ` + "`maintenance` field of responses."
)

func fail(err error) {
	fmt.Fprintf(os.Stderr, "error: %v", err)
	os.Exit(1)
}

func main() {
	fset := token.NewFileSet()
	pkgs := mylog.Check2(parser.ParseDir(fset, "../client", nil, parser.ParseComments))

	p := doc.New(pkgs["client"], "github.com/snapcore/snapd/client", 0)
	var errorKindT *doc.Type
	for _, t := range p.Types {
		if t.Name == "ErrorKind" {
			errorKindT = t
			break
		}
	}
	if errorKindT == nil {
		fail(fmt.Errorf("expected ErrorKind type not defined"))
	}
	for _, c := range errorKindT.Consts {
		if strings.HasPrefix(c.Doc, "Error kinds.") {
			fmt.Println(errorKindsHdr)
		} else if strings.HasPrefix(c.Doc, "Maintenance error kinds") {
			fmt.Println()
			fmt.Println(maintErrorKindsHdr)
		} else {
			fmt.Fprintf(os.Stderr, "unexpected error kind group: %v\n", c.Doc)
			continue
		}
		fmt.Println()

		kinds := make([]string, 0, len(c.Decl.Specs))
		docs := make(map[string]string, len(c.Decl.Specs))
		for _, spec := range c.Decl.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				fmt.Printf("%#v\n", spec)
				continue
			}
			kind := mylog.Check2(strconv.Unquote(vs.Values[0].(*ast.BasicLit).Value))

			// unexpected

			doc := vs.Doc.Text()
			name := vs.Names[0]
			pfx := name.String() + ":"
			if !strings.HasPrefix(doc, pfx) {
				fmt.Fprintf(os.Stderr, "expected %s: doc string prefix, got %q\n", name, doc)
			} else {
				doc = doc[len(pfx):]
			}
			doc = strings.Replace(doc, "\n", " ", -1)
			doc = strings.Replace(doc, "  ", " ", -1)
			doc = strings.TrimSpace(doc)
			if !strings.HasSuffix(doc, ".") {
				fmt.Fprintf(os.Stderr, "expected dot at the end %q for %s\n", doc, name)
			}
			if strings.HasPrefix(doc, "deprecated") {
				// skip
				continue
			}
			if doc == "" {
				doc = name.String() + "..."
			}
			kinds = append(kinds, kind)
			docs[kind] = doc
		}

		sort.Strings(kinds)
		for _, kind := range kinds {
			fmt.Printf("* `%s`: %s\n", kind, docs[kind])
		}
	}
}
