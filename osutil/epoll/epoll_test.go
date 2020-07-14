package epoll_test

import (
	"syscall"
	"testing"

	"github.com/snapcore/prompter/epoll"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type epollSuite struct{}

var _ = Suite(&epollSuite{})

func (*epollSuite) TestFlagMapping(c *C) {
	c.Check(epoll.Readable.ToSys(), Equals, syscall.EPOLLIN)
	c.Check(epoll.Writable.ToSys(), Equals, syscall.EPOLLOUT)
	c.Check((epoll.Readable | epoll.Writable).ToSys(), Equals, syscall.EPOLLIN|syscall.EPOLLOUT)

	c.Check(epoll.FromSys(syscall.EPOLLIN), Equals, epoll.Readable)
	c.Check(epoll.FromSys(syscall.EPOLLOUT), Equals, epoll.Writable)
	c.Check(epoll.FromSys(syscall.EPOLLIN|syscall.EPOLLOUT), Equals, epoll.Readable|epoll.Writable)
}
