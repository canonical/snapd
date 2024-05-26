package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"

	"github.com/ddkwork/golibrary/mylog"
)

var (
	matchDigit = regexp.MustCompile("[0-9]").Match
	matchAlpha = regexp.MustCompile("[a-zA-Z]").Match
)

func chOrder(ch uint8) int {
	// "~" is lower than everything else
	if ch == '~' {
		return -10
	}
	// empty is higher than "~" but lower than everything else
	if ch == 0 {
		return -5
	}
	if matchAlpha([]byte{ch}) {
		return int(ch)
	}

	// can only happen if cmpString sets '0' because there is no fragment
	if matchDigit([]byte{ch}) {
		return 0
	}

	return int(ch) + 256
}

func main() {
	var outFile string
	var pkgName string
	flag.StringVar(&outFile, "output", "-", "output file")
	flag.StringVar(&pkgName, "package", "foo", "package name")
	flag.Parse()

	out := os.Stdout
	if outFile != "" && outFile != "-" {

		out = mylog.Check2(os.Create(outFile))

		defer out.Close()
	}

	if pkgName == "" {
		pkgName = "foo"
	}

	fmt.Fprintln(out, "// auto-generated, DO NOT EDIT!")
	fmt.Fprintf(out, "package %v\n", pkgName)
	fmt.Fprintf(out, "\n")
	fmt.Fprintln(out, "var chOrder = [...]int{")
	for i := 0; i < 16; i++ {
		fmt.Fprintf(out, "\t")
		for j := 0; j < 16; j++ {
			if j != 0 {
				fmt.Fprintf(out, " ")
			}
			fmt.Fprintf(out, "%d,", chOrder(uint8(i*16+j)))

		}
		fmt.Fprintf(out, "\n")
	}
	fmt.Fprintln(out, "}")
}
