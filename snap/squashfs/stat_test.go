package squashfs

import (
	"fmt"
	"os"
	"time"

	. "gopkg.in/check.v1"
)

func (s *SquashfsTestSuite) TestStatModeBits(c *C) {
	for i := os.FileMode(0); i <= 0777; i++ {
		raw := []byte(fmt.Sprintf("%s user/group            53595 2017-12-08 11:19 ./yadda", i))

		expected := &stat{
			mode:  i,
			path:  "/yadda",
			user:  "user",
			group: "group",
			size:  int64(53595),
			mtime: time.Date(2017, 12, 8, 11, 19, 0, 0, time.Local),
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
