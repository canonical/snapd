package snappy

import (
	"log"
	"regexp"
	"strconv"
	"strings"
)

const (
	reDigit           = "[0-9]"
	reAlpha           = "[a-zA-Z]"
	reDigitOrNonDigit = "[0-9]+|[^0-9]+"

	reHasEpoch = "^[0-9]+:"
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
	if matched, _ := regexp.MatchString(reAlpha, string(ch)); matched {
		return int(ch)
	}

	// can only happen if cmpString sets '0' because there is no fragment
	if matched, _ := regexp.MatchString(reDigit, string(ch)); matched {
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
	re := regexp.MustCompile(reDigitOrNonDigit)
	matches := re.FindAllString(a, -1)
	return matches
}

func VersionIsValid(a string) bool {
	if matched, _ := regexp.MatchString(reHasEpoch, a); matched {
		return false
	}
	if strings.Count(a, "-") > 1 {
		return false
	}
	if strings.TrimSpace(a) == "" {
		return false
	}
	return true
}

func compareSubversion(va, vb string) int {
	frags_a := getFragments(va)
	frags_b := getFragments(vb)

	for i := 0; i < max(len(frags_a), len(frags_b)); i++ {
		a := "0"
		b := "0"
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

// compare two version strings and
// Returns:
//   -1 if a is smaller than b
//    0 if a equals b
//   +1 if a is bigger than b
func VersionCompare(va, vb string) (res int) {
	if !VersionIsValid(va) {
		log.Printf("Invalid version '%s', using '0' instead. Expect wrong results", va)
		va = "0"
	}
	if !VersionIsValid(vb) {
		log.Printf("Invalid version '%s', using '0' instead. Expect wrong results", vb)
		vb = "0"
	}

	if !strings.Contains(va, "-") {
		va += "-0"
	}
	if !strings.Contains(vb, "-") {
		vb += "-0"
	}

	// the main version number (before the "-")
	main_a := strings.Split(va, "-")[0]
	main_b := strings.Split(vb, "-")[0]
	res = compareSubversion(main_a, main_b)
	if res != 0 {
		return res
	}

	// the subversion revision behind the "-"
	rev_a := strings.Split(va, "-")[1]
	rev_b := strings.Split(vb, "-")[1]
	return compareSubversion(rev_a, rev_b)
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
