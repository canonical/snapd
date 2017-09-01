package spdx

import (
	"bufio"
	"io"
)

type Scanner struct {
	*bufio.Scanner
}

func spdxSplit(data []byte, atEOF bool) (advance int, token []byte, err error) {
	//println(len(data), string(data), atEOF)
	// skip WS
	start := 0
	for ; start < len(data); start++ {
		if data[start] != ' ' && data[start] != '\n' {
			break
		}
	}
	if start == len(data) {
		return start, nil, nil
	}

	// found (,)
	switch data[start] {
	case '(', ')':
		return start + 1, data[start : start+1], nil
	}

	// found non-ws, non-(), must be a token
	for i := start; i < len(data); i++ {
		switch data[i] {
		// token finished
		case ' ', '\n':
			return i + 1, data[start:i], nil
			// found (,) - we need to rescan it
		case '(', ')':
			return i, data[start:i], nil
		}
	}
	if atEOF && len(data) > start {
		return len(data), data[start:], nil
	}
	return start, nil, nil
}

func NewScanner(r io.Reader) *Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Split(spdxSplit)
	return &Scanner{scanner}
}
