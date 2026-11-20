// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package progress_test

import (
	"bytes"
	"fmt"
	"math"
	"strings"
	"time"

	"gopkg.in/check.v1"

	"github.com/mattn/go-runewidth"
	"github.com/snapcore/snapd/progress"
)

type ansiSuite struct{}

var _ = check.Suite(ansiSuite{})

func (ansiSuite) TestNorm(c *check.C) {
	msg := []rune(strings.Repeat("0123456789", 100))
	high := []rune("🤗🤗🤗🤗🤗")
	chinese := []rune("我The能quick吞下brown玻璃fox而jumps不over伤the身体lazy。dog")
	japanese := []rune("いろはにほへと ちりぬるを わかよたれそ つねならむ うゐのおくやま けふこえて あさきゆめみし ゑひもせす")
	korean := []rune("키스의 고유조건은 입술끼리 만나야 하고 특별한 기술은 필요치 않다")
	c.Assert(msg, check.HasLen, 1000)
	for i := 1; i < 1000; i += 1 {
		long := progress.Norm(i, msg)
		short := progress.Norm(i, nil)
		// a long message is truncated to fit
		c.Check(long, check.HasLen, i)
		c.Check(long[len(long)-1], check.Equals, rune('…'))
		// a short message is padded to width
		c.Check(short, check.HasLen, i)
		c.Check(string(short), check.Equals, strings.Repeat(" ", i))
		// high unicode? no problem
		c.Check(runewidth.StringWidth(string(progress.Norm(i, high))), check.Equals, i)
		// CJK
		c.Check(runewidth.StringWidth(string(progress.Norm(i, chinese))), check.Equals, i)
		c.Check(runewidth.StringWidth(string(progress.Norm(i, japanese))), check.Equals, i)
		c.Check(runewidth.StringWidth(string(progress.Norm(i, korean))), check.Equals, i)
	}
	// check it doesn't panic for negative nor zero widths
	c.Check(progress.Norm(0, []rune("hello")), check.HasLen, 0)
	c.Check(progress.Norm(-10, []rune("hello")), check.HasLen, 0)
}

func (ansiSuite) TestPercent(c *check.C) {
	p := &progress.ANSIMeter{}
	for i := -1000.; i < 1000.; i += 5 {
		p.SetTotal(i)
		for j := -1000.; j < 1000.; j += 3 {
			p.SetWritten(j)
			percent := p.Percent()
			c.Check(percent, check.HasLen, 4)
			c.Check(percent[len(percent)-1:], check.Equals, "%")
		}
	}
}

func (ansiSuite) TestStart(c *check.C) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockTermWidth(func() int { return 80 })()

	p := &progress.ANSIMeter{}
	p.Start("0123456789", 100)
	c.Check(p.GetTotal(), check.Equals, 100.)
	c.Check(p.GetWritten(), check.Equals, 0.)
	c.Check(buf.String(), check.Equals, progress.CursorInvisible)
}

func (ansiSuite) TestFinish(c *check.C) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockTermWidth(func() int { return 80 })()
	p := &progress.ANSIMeter{}
	p.Finished()
	c.Check(buf.String(), check.Equals, fmt.Sprint(
		"\r",                       // move cursor to start of line
		progress.ExitAttributeMode, // turn off color, reverse, bold, anything
		progress.CursorVisible,     // turn the cursor back on
		progress.ClrEOL,            // and clear the rest of the line
	))
}

func (ansiSuite) TestSetLayout(c *check.C) {
	var buf bytes.Buffer
	var width int
	defer progress.MockStdout(&buf)()
	defer progress.MockEmptyEscapes()()
	defer progress.MockTermWidth(func() int { return width })()

	p := &progress.ANSIMeter{}
	msg := "0123456789"
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	p.Start(msg, 1e300)
	for i := 1; i <= 80; i++ {
		desc := check.Commentf("width %d", i)
		width = i
		buf.Reset()
		<-ticker.C
		p.Set(float64(i))
		out := buf.String()
		c.Check([]rune(out), check.HasLen, i+1, desc)
		switch {
		case i < len(msg):
			c.Check(out, check.Equals, "\r"+msg[:i-1]+"…", desc)
		case i <= 15:
			c.Check(out, check.Equals, fmt.Sprintf("\r%*s", -i, msg), desc)
		case i <= 20:
			c.Check(out, check.Equals, fmt.Sprintf("\r%*s ages!", -(i-6), msg), desc)
		case i <= 29:
			c.Check(out, check.Equals, fmt.Sprintf("\r%*s   0%% ages!", -(i-11), msg), desc)
		default:
			c.Check(out, check.Matches, fmt.Sprintf("\r%*s   0%%  [ 0-9]{4}B/s ages!", -(i-20), msg), desc)
		}
	}
}

func (ansiSuite) TestSetLayoutMultibyte(c *check.C) {
	var buf bytes.Buffer
	var duration string
	var msg = "0123456789"
	const termWidth = 80
	defer progress.MockStdout(&buf)()
	defer progress.MockEmptyEscapes()()
	defer progress.MockTermWidth(func() int { return termWidth })()
	defer progress.MockFormatDuration(func(_ float64) string {
		return duration
	})()

	for _, dstr := range []string{"м", "語"} {
		duration = dstr
		buf.Reset()

		p := &progress.ANSIMeter{}
		p.Start(msg, 1e300)
		p.Set(0.99 * 1e300)
		out := buf.String()
		c.Check(runewidth.StringWidth(out), check.Equals, termWidth, check.Commentf("unexpected width: %v", runewidth.StringWidth(out)))
		c.Check(out, check.Matches,
			fmt.Sprintf("\r0123456789 \\s+  99%% +[0-9]+(\\.[0-9]+)?[kMGTPEZY]?B/s %s", dstr))
	}
}

func (ansiSuite) TestSetEscapes(c *check.C) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockSimpleEscapes()()
	defer progress.MockTermWidth(func() int { return 10 })()

	p := &progress.ANSIMeter{}
	msg := "0123456789"
	p.Start(msg, 10)
	for i := 0.; i <= 10; i++ {
		buf.Reset()
		p.Set(i)
		// here we're using the fact that the message has the same
		// length as p's total to make the test simpler :-)
		expected := "\r<MR>" + msg[:int(i)] + "<ME>" + msg[int(i):]
		c.Check(buf.String(), check.Equals, expected, check.Commentf("%g", i))
	}
}

func (ansiSuite) TestSpin(c *check.C) {
	termWidth := 9
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockSimpleEscapes()()
	defer progress.MockTermWidth(func() int { return termWidth })()

	p := &progress.ANSIMeter{}
	msg := "0123456789"
	c.Assert(len(msg), check.Equals, 10)
	p.Start(msg, 10)

	// term too narrow to fit msg
	for i, s := range progress.Spinner {
		buf.Reset()
		p.Spin(msg)
		expected := "\r" + msg[:8] + "…"
		c.Check(buf.String(), check.Equals, expected, check.Commentf("%d (%s)", i, s))
	}

	// term fits msg but not spinner
	termWidth = 11
	for i, s := range progress.Spinner {
		buf.Reset()
		p.Spin(msg)
		expected := "\r" + msg + " "
		c.Check(buf.String(), check.Equals, expected, check.Commentf("%d (%s)", i, s))
	}

	// term fits msg and spinner
	termWidth = 12
	for i, s := range progress.Spinner {
		buf.Reset()
		p.Spin(msg)
		expected := "\r" + msg + " " + s
		c.Check(buf.String(), check.Equals, expected, check.Commentf("%d (%s)", i, s))
	}
}

func (ansiSuite) TestNotify(c *check.C) {
	var buf bytes.Buffer
	var width int
	defer progress.MockStdout(&buf)()
	defer progress.MockSimpleEscapes()()
	defer progress.MockTermWidth(func() int { return width })()

	p := &progress.ANSIMeter{}
	p.Start("working", 1e300)

	width = 10
	p.Set(0)
	p.Notify("hello there")
	p.Set(1)
	c.Check(buf.String(), check.Equals, "<VI>"+ // the VI from Start()
		"\r<MR><ME>working   "+ // the Set(0)
		"\r<ME><CE>hello\n"+ // first line of the Notify (note it wrapped at word end)
		"there\n"+
		"\r<MR><ME>working   ") // the Set(1)

	buf.Reset()
	p.Set(0)
	p.Notify("supercalifragilisticexpialidocious")
	p.Set(1)
	c.Check(buf.String(), check.Equals, ""+ // no Start() this time
		"\r<MR><ME>working   "+ // the Set(0)
		"\r<ME><CE>supercalif\n"+ // the Notify, word is too long so it's just split
		"ragilistic\n"+
		"expialidoc\n"+
		"ious\n"+
		"\r<MR><ME>working   ") // the Set(1)

	buf.Reset()
	width = 16
	p.Set(0)
	p.Notify("hello there")
	p.Set(1)
	c.Check(buf.String(), check.Equals, ""+ // no Start()
		"\r<MR><ME>working    ages!"+ // the Set(0)
		"\r<ME><CE>hello there\n"+ // first line of the Notify (no wrap!)
		"\r<MR><ME>working    ages!") // the Set(1)

}

func (ansiSuite) TestWrite(c *check.C) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockSimpleEscapes()()
	defer progress.MockTermWidth(func() int { return 10 })()

	p := &progress.ANSIMeter{}
	p.Start("123456789x", 10)
	for i := 0; i < 10; i++ {
		n, err := fmt.Fprintf(p, "%d", i)
		c.Assert(err, check.IsNil)
		c.Check(n, check.Equals, 1)
	}

	c.Check(buf.String(), check.Equals, strings.Join([]string{
		"<VI>", // Start()
		"\r<MR>1<ME>23456789x",
		"\r<MR>12<ME>3456789x",
		"\r<MR>123<ME>456789x",
		"\r<MR>1234<ME>56789x",
		"\r<MR>12345<ME>6789x",
		"\r<MR>123456<ME>789x",
		"\r<MR>1234567<ME>89x",
		"\r<MR>12345678<ME>9x",
		"\r<MR>123456789<ME>x",
		"\r<MR>123456789x<ME>",
	}, ""))
}

func (ansiSuite) TestSetZeroTotal(c *check.C) {
	testSetInvalidTotal(c, 0)
}

func (ansiSuite) TestSetNegativeTotal(c *check.C) {
	testSetInvalidTotal(c, -5)
}

func (ansiSuite) TestSetNaNTotal(c *check.C) {
	testSetInvalidTotal(c, math.NaN())
}

func (ansiSuite) TestSetInfiniteTotal(c *check.C) {
	for _, total := range []float64{math.Inf(1), math.Inf(-1)} {
		testSetInvalidTotal(c, total)
	}
}

func testSetInvalidTotal(c *check.C, total float64) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockSimpleEscapes()()
	defer progress.MockTermWidth(func() int { return 40 })()

	p := &progress.ANSIMeter{}
	p.Start("work", total)
	for _, current := range []float64{0, 5} {
		buf.Reset()
		p.Set(current)
		desc := check.Commentf("total %g, current %g", total, current)
		c.Check(p.GetWritten(), check.Equals, 0., desc)
		c.Check(p.Percent(), check.Equals, "---%", desc)
		c.Check(buf.String(), check.Equals, "\r<MR><ME>work                                ---%", desc)
	}
}

func (ansiSuite) TestSetNonPositiveTotalLayout(c *check.C) {
	var buf bytes.Buffer
	var width int
	defer progress.MockStdout(&buf)()
	defer progress.MockSimpleEscapes()()
	defer progress.MockTermWidth(func() int { return width })()

	p := &progress.ANSIMeter{}
	p.Start("work", 0)
	for _, width = range []int{10, 15, 16, 20, 21, 29, 30, 40} {
		buf.Reset()
		p.Set(5)
		want := "\r<MR><ME>work"
		if width <= 20 {
			want += strings.Repeat(" ", width-4)
		} else {
			want += strings.Repeat(" ", width-9) + " ---%"
		}
		c.Check(buf.String(), check.Equals, want, check.Commentf("width %d", width))
	}
}

func (ansiSuite) TestSetTotalInvalid(c *check.C) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockTermWidth(func() int { return 40 })()

	p := &progress.ANSIMeter{}
	p.Start("work", 10)
	for _, total := range []float64{0, -5, math.NaN(), math.Inf(1), math.Inf(-1)} {
		p.SetTotal(total)
		p.Set(3)
		desc := check.Commentf("total %g", total)
		c.Check(p.GetWritten(), check.Equals, 0., desc)
		c.Check(p.Percent(), check.Equals, "---%", desc)
	}
}

func (ansiSuite) TestWriteZeroTotal(c *check.C) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockTermWidth(func() int { return 10 })()

	p := &progress.ANSIMeter{}
	p.Start("work", 0)
	n, err := fmt.Fprintf(p, "hello")
	c.Check(err, check.IsNil)
	c.Check(n, check.Equals, 5)
	c.Check(p.GetWritten(), check.Equals, 0.)
}

func (ansiSuite) TestSetCurrentClamps(c *check.C) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()
	defer progress.MockTermWidth(func() int { return 10 })()

	p := &progress.ANSIMeter{}
	p.Start("work", 10)
	for _, tc := range []struct {
		current, written float64
	}{
		{-5, 0},
		{100, 10},
		{math.NaN(), 0},
		{math.Inf(1), 10},
	} {
		p.Set(tc.current)
		c.Check(p.GetWritten(), check.Equals, tc.written, check.Commentf("current %g", tc.current))
	}
}
