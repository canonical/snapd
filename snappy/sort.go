package snappy

import (
	"regexp"
	"strconv"
	"strings"
)

const (
	RE_DIGIT = "[0-9]"
	RE_ALPHA = "[a-zA-Z]"
)

// golang: seriously? that's sad!
func max(a, b int) int {
   if a < b {
      return b
   }
   return a
}

// version number compare, inspired by the libapt/python-debian code
func cmpInt(int_a, int_b int) int {
	if int_a < int_b {
		return -1
	} else if int_a > int_b {
		return 1
	} 
	return 0
}

func order(ch uint8) int {
	var err error
	
	if ch == '~' {
		return -1
	}
	if _, err = regexp.MatchString(string(ch), RE_DIGIT); err == nil {
		v, _ := strconv.Atoi(string(ch))
		return v
	}
	if _, err = regexp.MatchString(string(ch), RE_ALPHA); err == nil {
		return int(ch)
	}

	return int(ch) + 256
}

func cmpString(as, bs string) int {
	for i := 0; i < max(len(as), len(bs)); i++ {
		a := uint8('0')
		b := uint8('0')
		if i < len(as) {
			a = as[i]
		}
		if i < len(bs) {
			b = bs[i]
		}
		if order(a) < order(b) {
			return -1
		}
		if order(a) > order(b) {
			return +1
		}
	}
	return 0
}

func cmpFragment(a, b string) int {
	int_a, err_a := strconv.Atoi(a)
	int_b, err_b := strconv.Atoi(b)
	if err_a == nil && err_b == nil {
		return cmpInt(int_a, int_b)
	}
	return cmpString(a, b)
}

func getFragments(a string) []string {
	return strings.Split(a, ".")
}

func VersionCompare(a, b string) int {
	frags_a := getFragments(a)
	frags_b := getFragments(b)

	for i := 0; i < max(len(frags_a), len(frags_b)); i++ {
		a = "0"
		b = "0"
		if i < len(frags_a) {
			a = frags_a[i]
		}
		if i < len(frags_b) {
			b = frags_b[i]
		}
		res := cmpFragment(a, b)
		if res != 0 {
			return res
		}
	}
	return 0
}
