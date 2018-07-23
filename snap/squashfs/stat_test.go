package squashfs

import (
	"fmt"
	"math"
	"os"
	"time"

	. "gopkg.in/check.v1"
)

func (s *SquashfsTestSuite) TestStatBadNodes(c *C) {
	badlines := map[string][]string{
		"node": {
			// size, but device
			"brwxrwxr-x u/u             53595 2017-12-08 11:19 .",
			"crwxrwxr-x u/u             53595 2017-12-08 11:19 .",
			// node info is noise
			"brwxrwxr-x u/u             noise 2017-12-08 11:19 .",
			"crwxrwxr-x u/u             noise 2017-12-08 11:19 .",
			// major is noise
			"brwxrwxr-x u/u             noise, 1 2017-12-08 11:19 .",
			"crwxrwxr-x u/u             noise, 1 2017-12-08 11:19 .",
			// minor is noise
			"brwxrwxr-x u/u             1, noise 2017-12-08 11:19 .",
			"crwxrwxr-x u/u             1, noise 2017-12-08 11:19 .",
		},
		"size": {
			// size is noise
			"drwxrwxr-x u/g             noise 2017-12-08 11:19 .",
			"drwxrwxr-x u/g             1noise 2017-12-08 11:19 .",
			// size too big
			"drwxrwxr-x u/g             36893488147419103232 2017-12-08 11:19 .",
		},
		"line": {
			// shorter than the minimum:
			"-rw-r--r-- too/short 20 2017-12-08 11:19 ./",
			// truncated:
			"drwxrwxr-x",
			"drwxrwxr-x ",
			"drwxrwxr-x u/u",
			"drwxrwxr-x u/g ",
			"drwxrwxr-x u/g             53595",
			"drwxrwxr-x u/g             53595 ",
			"drwxrwxr-x u/g             53595 2017-12-08 11:19",
			"drwxrwxr-x u/g             53595 2017-12-08 11:19 ",

			// mode keeps on going
			"drwxrwxr-xr-x u/g             53595 2017-12-08 11:19 .",

			// spurious padding:
			"drwxrwxr-x  u/u             53595 2017-12-08 11:19 .",

			// missing size
			"drwxrwxr-x u/u                    2017-12-08 11:19 ",
			// everything is size
			"drwxrwxr-x u/u 111111111111111111111111111111111111",
		},
		"mode": {
			// zombie file type?
			"zrwxrwxr-x u/g             53595 2017-12-08 11:19 .",
			// strange permissions
			"dbwxrwxr-x u/g             53595 2017-12-08 11:19 .",
			"drbxrwxr-x u/g             53595 2017-12-08 11:19 .",
			"drwbrwxr-x u/g             53595 2017-12-08 11:19 .",
			"drwxbwxr-x u/g             53595 2017-12-08 11:19 .",
			"drwxrbxr-x u/g             53595 2017-12-08 11:19 .",
			"drwxrwbr-x u/g             53595 2017-12-08 11:19 .",
			"drwxrwxb-x u/g             53595 2017-12-08 11:19 .",
			"drwxrwxrbx u/g             53595 2017-12-08 11:19 .",
			"drwxrwxr-b u/g             53595 2017-12-08 11:19 .",
		},
		"owner": {
			"-rw-r--r-- some.user.with.a.much.too.long.name/some.group.with.a.much.too.long.name               20 2017-12-08 11:19 ./foo",
			"-rw-r--r-- nogroup/               20 2017-12-08 11:19 ./foo",
			"-rw-r--r-- noslash               20 2017-12-08 11:19 ./foo",
			"-rw-r--r-- this.line.finishes.before.finishing.owner",
		},
		"time": {
			// time is bonkers:
			"drwxrwxr-x u/u             53595 2017-bonkers-what .",
		},
		"path": {
			// path doesn't start with "."
			"drwxrwxr-x u/g             53595 2017-12-08 11:19 foo",
		},
	}
	for kind, lines := range badlines {
		for _, line := range lines {
			com := Commentf("%q (expected bad %s)", line, kind)
			st, err := fromRaw([]byte(line))
			c.Assert(err, NotNil, com)
			c.Check(st, IsNil, com)
			c.Check(err, ErrorMatches, fmt.Sprintf("cannot parse %s: .*", kind))
		}
	}
}

func (s *SquashfsTestSuite) TestStatUserGroup(c *C) {
	usergroups := [][2]string{
		{"u", "g"},
		{"user", "group"},
		{"some.user.with.a.veery.long.name", "group"},
		{"user", "some.group.with.a.very.long.name"},
		{"some.user.with.a.veery.long.name", "some.group.with.a.very.long.name"},
	}
	for _, ug := range usergroups {
		user, group := ug[0], ug[1]
		raw := []byte(fmt.Sprintf("-rw-r--r-- %s/%s               20 2017-12-08 11:19 ./foo", user, group))

		expected := &stat{
			mode:  0644,
			path:  "/foo",
			user:  user,
			group: group,
			size:  20,
			mtime: time.Date(2017, 12, 8, 11, 19, 0, 0, time.UTC),
		}

		com := Commentf("%q", raw)
		c.Assert(len(user) <= 32, Equals, true, com)
		c.Assert(len(group) <= 32, Equals, true, com)

		st, err := fromRaw(raw)
		c.Assert(err, IsNil, com)
		c.Check(st, DeepEquals, expected, com)
	}
}

func (s *SquashfsTestSuite) TestStatPath(c *C) {
	paths := [][]byte{
		[]byte("hello"),
		[]byte(" this is/ a path/(somehow)"),
		{239, 191, 190},
		{0355, 0240, 0200, 0355, 0260, 0200},
	}
	for _, path := range paths {
		raw := []byte(fmt.Sprintf("-rw-r--r-- user/group               20 2017-12-08 11:19 ./%s", path))
		expected := &stat{
			mode:  0644,
			path:  fmt.Sprintf("/%s", path),
			user:  "user",
			group: "group",
			size:  20,
			mtime: time.Date(2017, 12, 8, 11, 19, 0, 0, time.UTC),
		}

		com := Commentf("%q", raw)
		st, err := fromRaw(raw)
		c.Assert(err, IsNil, com)
		c.Check(st, DeepEquals, expected, com)
	}
}

func (s *SquashfsTestSuite) TestStatBlock(c *C) {
	line := "brw-rw---- root/disk             7,  0 2017-12-05 10:29 ./dev/loop0"
	st, err := fromRaw([]byte(line))
	c.Assert(err, IsNil)
	c.Check(st, DeepEquals, &stat{
		mode:  0660 | os.ModeDevice,
		path:  "/dev/loop0",
		user:  "root",
		group: "disk",
		mtime: time.Date(2017, 12, 5, 10, 29, 0, 0, time.UTC),
	})
	// note the major and minor numbers are ignored (for now)
}

func (s *SquashfsTestSuite) TestStatCharacter(c *C) {
	line := "crw-rw---- root/audio           14,  3 2017-12-05 10:29 ./dev/dsp"
	st, err := fromRaw([]byte(line))
	c.Assert(err, IsNil)
	c.Check(st, DeepEquals, &stat{
		mode:  0660 | os.ModeCharDevice,
		path:  "/dev/dsp",
		user:  "root",
		group: "audio",
		mtime: time.Date(2017, 12, 5, 10, 29, 0, 0, time.UTC),
	})
	// note the major and minor numbers are ignored (for now)
}

func (s *SquashfsTestSuite) TestStatSymlink(c *C) {
	line := "lrwxrwxrwx root/root                 4 2017-12-05 10:29 ./var/run -> /run"
	st, err := fromRaw([]byte(line))
	c.Assert(err, IsNil)
	c.Check(st, DeepEquals, &stat{
		mode:  0777 | os.ModeSymlink,
		path:  "/var/run",
		user:  "root",
		group: "root",
		size:  4,
		mtime: time.Date(2017, 12, 5, 10, 29, 0, 0, time.UTC),
	})
}

func (s *SquashfsTestSuite) TestStatNamedPipe(c *C) {
	line := "prw-rw-r-- john/john                 0 2018-01-09 10:24 ./afifo"
	st, err := fromRaw([]byte(line))
	c.Assert(err, IsNil)
	c.Check(st, DeepEquals, &stat{
		mode:  0664 | os.ModeNamedPipe,
		path:  "/afifo",
		user:  "john",
		group: "john",
		mtime: time.Date(2018, 1, 9, 10, 24, 0, 0, time.UTC),
	})
}

func (s *SquashfsTestSuite) TestStatSocket(c *C) {
	line := "srwxrwxr-x john/john                 0 2018-01-09 10:24 ./asock"
	st, err := fromRaw([]byte(line))
	c.Assert(err, IsNil)
	c.Check(st, DeepEquals, &stat{
		mode:  0775 | os.ModeSocket,
		path:  "/asock",
		user:  "john",
		group: "john",
		mtime: time.Date(2018, 1, 9, 10, 24, 0, 0, time.UTC),
	})
}

func (s *SquashfsTestSuite) TestStatLength(c *C) {
	ns := []int64{
		0,
		1024,
		math.MaxInt32,
		math.MaxInt64,
	}
	for _, n := range ns {
		raw := []byte(fmt.Sprintf("-rw-r--r-- user/group %16d 2017-12-08 11:19 ./some filename", n))
		expected := &stat{
			mode:  0644,
			path:  "/some filename",
			user:  "user",
			group: "group",
			size:  n,
			mtime: time.Date(2017, 12, 8, 11, 19, 0, 0, time.UTC),
		}

		com := Commentf("%q", raw)
		st, err := fromRaw(raw)
		c.Assert(err, IsNil, com)
		c.Check(st, DeepEquals, expected, com)
	}
}

func (s *SquashfsTestSuite) TestStatModeBits(c *C) {
	for i := os.FileMode(0); i <= 0777; i++ {
		raw := []byte(fmt.Sprintf("%s user/group            53595 2017-12-08 11:19 ./yadda", i))

		expected := &stat{
			mode:  i,
			path:  "/yadda",
			user:  "user",
			group: "group",
			size:  int64(53595),
			mtime: time.Date(2017, 12, 8, 11, 19, 0, 0, time.UTC),
		}

		com := Commentf("%q vs %o", raw, i)
		st, err := fromRaw(raw)
		c.Assert(err, IsNil, com)
		c.Check(st, DeepEquals, expected, com)

		jRaw := make([]byte, len(raw))

		for j := 01000 + i; j <= 07777; j += 01000 {
			// this silliness only needed because os.FileMode's String() throws away sticky/setuid/setgid bits
			copy(jRaw, raw)
			expected.mode = j
			if j&01000 != 0 {
				if j&0001 != 0 {
					jRaw[9] = 't'
				} else {
					jRaw[9] = 'T'
				}
			}
			if j&02000 != 0 {
				if j&0010 != 0 {
					jRaw[6] = 's'
				} else {
					jRaw[6] = 'S'
				}
			}
			if j&04000 != 0 {
				if j&0100 != 0 {
					jRaw[3] = 's'
				} else {
					jRaw[3] = 'S'
				}

				com := Commentf("%q vs %o", jRaw, j)
				st, err := fromRaw(jRaw)
				c.Assert(err, IsNil, com)
				c.Check(st, DeepEquals, expected, com)

			}
		}
	}
}
