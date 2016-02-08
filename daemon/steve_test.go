package daemon

import (
	. "gopkg.in/check.v1"

	"gopkg.in/tomb.v2"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/progress"
)

type steveSuite struct {}

var _ = Suite(&steveSuite{})

func (s *steveSuite) SetUpSuite(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *steveSuite) TestSteve(c *C) {
	d := newTestDaemon()

	ch1 := make(chan struct{})
	ch2 := make(chan struct{})

	p := &progress.SimpleProgress{}
	p.Start("test", 50)

	task := d.AddTask(func() interface{} {
		ch1 <- struct{}{}
		p.Set(10.0)
		ch1 <- struct{}{}
		return "hello"
	})

	t := task.Tomb()

	go func() {
		ch2 <- struct{}{}
		<-t.Dead()
		p.Set(50.0)
		ch2 <- struct{}{}
	}()

	c.Assert(p.Percentage(), Equals, 0.0)
	c.Assert(t.Err(), Equals, tomb.ErrStillAlive)
	<-ch1
	c.Assert(p.Percentage(), Equals, 20.0)
	c.Assert(t.Err(), Equals, tomb.ErrStillAlive)
	<-ch1
	<-ch2
	c.Assert(t.Err(), Equals, tomb.ErrStillAlive)
	<-ch2
	c.Assert(t.Err(), IsNil)
	c.Assert(p.Percentage(), Equals, 100.0)
}
