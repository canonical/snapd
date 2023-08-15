package listener_test

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type listenerSuite struct{}

var _ = Suite(&listenerSuite{})
