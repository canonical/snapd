package snappy

import (
	"regexp"
	"strconv"
)

const (
	RE_DIGIT              = "[0-9]"
	RE_ALPHA              = "[a-zA-Z]"
	RE_DIGIT_OR_NON_DIGIT = "[0-9]+|[^0-9]+"
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

func chOrder(ch uint8) int {
	if ch == '~' {
		return -1
	}
	if matched, _ := regexp.MatchString(RE_ALPHA, string(ch)); matched {
		return int(ch)
	}

	// can only happen if cmpString sets '0' because there is no fragment
	if matched, _ := regexp.MatchString(RE_DIGIT, string(ch)); matched {
		return 0
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
		if chOrder(a) < chOrder(b) {
			return -1
		}
		if chOrder(a) > chOrder(b) {
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
	res := cmpString(a, b)
	//fmt.Println(a, b, res)
	return res
}

func getFragments(a string) []string {
	re := regexp.MustCompile(RE_DIGIT_OR_NON_DIGIT)
	matches := re.FindAllString(a, -1)
	return matches
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
		//fmt.Println(a, b, res)
		if res != 0 {
			return res
		}
	}
	return 0
}

// sort interface
type ByVersion []string

func (bv ByVersion) Less(a, b int) bool {
	return (VersionCompare(bv[a], bv[b]) < 0)
}
func (bv ByVersion) Swap(a, b int) {
	bv[a], bv[b] = bv[b], bv[a]
}
func (bv ByVersion) Len() int {
	return len(bv)
}
