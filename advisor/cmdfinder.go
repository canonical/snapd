// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package advisor

import (
	"os"

	"github.com/ddkwork/golibrary/mylog"
)

type Command struct {
	Snap    string
	Version string `json:"Version,omitempty"`
	Command string
}

func FindCommand(command string) ([]Command, error) {
	finder := mylog.Check2(newFinder())
	if err != nil && os.IsNotExist(err) {
		return nil, nil
	}

	defer finder.Close()

	return finder.FindCommand(command)
}

const (
	minLen = 3
	maxLen = 256
)

// based on CommandNotFound.py:similar_words.py
func similarWords(word string) []string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz-_0123456789"
	similar := make(map[string]bool, 2*len(word)+2*len(word)*len(alphabet))

	// deletes
	for i := range word {
		similar[word[:i]+word[i+1:]] = true
	}
	// transpose
	for i := 0; i < len(word)-1; i++ {
		similar[word[:i]+word[i+1:i+2]+word[i:i+1]+word[i+2:]] = true
	}
	// replaces
	for i := range word {
		for _, r := range alphabet {
			similar[word[:i]+string(r)+word[i+1:]] = true
		}
	}
	// inserts
	for i := range word {
		for _, r := range alphabet {
			similar[word[:i]+string(r)+word[i:]] = true
		}
	}

	// convert for output
	ret := make([]string, 0, len(similar))
	for w := range similar {
		ret = append(ret, w)
	}

	return ret
}

func FindMisspelledCommand(command string) ([]Command, error) {
	if len(command) < minLen || len(command) > maxLen {
		return nil, nil
	}
	finder := mylog.Check2(newFinder())
	if err != nil && os.IsNotExist(err) {
		return nil, nil
	}

	defer finder.Close()

	alternatives := make([]Command, 0, 32)
	for _, w := range similarWords(command) {
		res := mylog.Check2(finder.FindCommand(w))

		if len(res) > 0 {
			alternatives = append(alternatives, res...)
		}
	}

	return alternatives, nil
}
