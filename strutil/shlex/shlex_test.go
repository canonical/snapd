/*
Copyright 2012 Google Inc. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package shlex

import (
	"errors"
	"strings"
	"testing"

	"github.com/ddkwork/golibrary/mylog"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

// one two "three four" "five \"six\"" seven#eight # nine # ten
// eleven 'twelve\'
var testString = "\\one two \"three four\" \"five \\\"six\\\"\" seven#eight # nine # ten\n eleven 'twelve\\' thirteen=13 fourteen/14"

func TestClassifier(t *testing.T) {
	classifier := newDefaultClassifier()
	tests := map[rune]runeTokenClass{
		' ':  spaceRuneClass,
		'"':  escapingQuoteRuneClass,
		'\'': nonEscapingQuoteRuneClass,
		'#':  commentRuneClass,
	}
	for runeChar, want := range tests {
		got := classifier.ClassifyRune(runeChar)
		if got != want {
			t.Errorf("ClassifyRune(%v) -> %v. Want: %v", runeChar, got, want)
		}
	}
}

func TestTokenizer(t *testing.T) {
	testInput := strings.NewReader(testString)
	expectedTokens := []*Token{
		{WordToken, "one"},
		{WordToken, "two"},
		{WordToken, "three four"},
		{WordToken, "five \"six\""},
		{WordToken, "seven#eight"},
		{CommentToken, " nine # ten"},
		{WordToken, "eleven"},
		{WordToken, "twelve\\"},
		{WordToken, "thirteen=13"},
		{WordToken, "fourteen/14"},
	}

	tokenizer := NewTokenizer(testInput)
	for i, want := range expectedTokens {
		got := mylog.Check2(tokenizer.Next())

		if !got.Equal(want) {
			t.Errorf("Tokenizer.Next()[%v] of %q -> %v. Want: %v", i, testString, got, want)
		}
	}
}

func TestLexer(t *testing.T) {
	testInput := strings.NewReader(testString)
	expectedStrings := []string{"one", "two", "three four", "five \"six\"", "seven#eight", "eleven", "twelve\\", "thirteen=13", "fourteen/14"}

	lexer := NewLexer(testInput)
	for i, want := range expectedStrings {
		got := mylog.Check2(lexer.Next())

		if got != want {
			t.Errorf("Lexer.Next()[%v] of %q -> %v. Want: %v", i, testString, got, want)
		}
	}
}

func TestSplit(t *testing.T) {
	want := []string{"one", "two", "three four", "five \"six\"", "seven#eight", "eleven", "twelve\\", "thirteen=13", "fourteen/14"}
	got := mylog.Check2(Split(testString))

	if len(want) != len(got) {
		t.Errorf("Split(%q) -> %v. Want: %v", testString, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Split(%q)[%v] -> %v. Want: %v", testString, i, got[i], want[i])
		}
	}
}

func TestEOFAfterEscape(t *testing.T) {
	_ := mylog.Check2(Split(testString + "\\"))
	if err == nil {
		t.Error(err)
	}
}

func TestEOFInQuotingEscape(t *testing.T) {
	_ := mylog.Check2(Split(`foo"`))
	if err == nil {
		t.Error(err)
	}

	_ = mylog.Check2(Split(`foo'`))
	if err == nil {
		t.Error(err)
	}

	_ = mylog.Check2(Split(`"foo\`))
	if err == nil {
		t.Error(err)
	}
}

func TestEOFInComment(t *testing.T) {
	got := mylog.Check2(Split("#"))

	if len(got) > 1 {
		t.Errorf("Split(%q) -> %v", testString, got)
	}
}

type nastyReader struct{}

var nastyReaderErr = errors.New("foo")

func (*nastyReader) Read(_ []byte) (int, error) {
	return 0, nastyReaderErr
}

func TestNastyReader(t *testing.T) {
	l := NewLexer(&nastyReader{})
	_ := mylog.Check2(l.Next())
	if err == nil {
		t.Errorf("expected an error, got nil instead")
	}
	if err != nastyReaderErr {
		t.Errorf("unexpected error")
	}
}
