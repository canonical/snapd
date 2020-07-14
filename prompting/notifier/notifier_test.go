package notifier_test

import (
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type notifierSuite struct{}

var _ = Suite(&notifierSuite{})
