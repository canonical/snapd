package testutil_test

import (
	"fmt"

	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

func ExampleBackup_mockingSimple() {

	mockable := func() {
		fmt.Println("Original")
	}

	// Mock
	restore := testutil.Backup(&mockable)
	mockable = func() {
		fmt.Println("Mock")
	}

	// Restore
	restore()

	mockable()

	// Output: Original
}

func ExampleBackup_mockingMultiple() {
	mockableFunc := func() {
		fmt.Println("Original function")
	}
	mockableNumber := 17.53
	mockableString := "Original string"
	mockableStruct := struct {
		first  string
		second string
	}{
		first:  "Original",
		second: "struct",
	}

	// Mock
	restore := testutil.BackupMany(&mockableFunc, &mockableNumber, &mockableString, &mockableStruct)
	mockableFunc = func() {
		fmt.Println("Mock")
	}
	mockableNumber = 37
	mockableString = "Mock"
	mockableStruct.first, mockableStruct.second = "mocked", "value"

	// Restore
	restore()

	mockableFunc()
	fmt.Println(mockableNumber, mockableString, mockableStruct)

	// Output:
	// Original function
	// 17.53 Original string {Original struct}
}

var _ = Suite(&MockingTestSuite{})

type MockingTestSuite struct{}

func (s *MockingTestSuite) TestBackup(c *C) {
	mockableInt := 2
	mockableString := "foo"
	mockableFunc := func() string { return "original" }

	restore := testutil.Backup(&mockableInt)
	mockableInt = 42
	restore()
	c.Check(mockableInt, Equals, 2)

	restore = testutil.Backup(&mockableString)
	mockableString = "bar"
	restore()
	c.Check(mockableString, Equals, "foo")

	restore = testutil.Backup(&mockableFunc)
	mockableFunc = func() string { return "mock" }
	restore()
	c.Check(mockableFunc(), Equals, "original")
}

func (s *MockingTestSuite) TestBackupMany(c *C) {
	mockable1 := "foo"
	mockable2 := 2
	mockable3 := func() string { return "foo" }

	restore := testutil.BackupMany(&mockable1, &mockable2, &mockable3)
	mockable1 = "bar"
	mockable2 = 42
	mockable3 = func() string { return "bar" }

	restore()
	c.Check(mockable1, Equals, "foo")
	c.Check(mockable2, Equals, 2)
	c.Check(mockable3(), Equals, "foo")
}

func (s *MockingTestSuite) TestMock(c *C) {
	mockableInt := 2
	mockableString := "foo"
	mockableFunc := func() string { return "foo" }

	type Foo struct {
		Bar int
	}

	mockableStruct := Foo{Bar: 9}

	restore := testutil.Mock(&mockableInt, 5)
	c.Check(mockableInt, Equals, 5)
	restore()
	c.Check(mockableInt, Equals, 2)

	restore = testutil.Mock(&mockableString, "bar")
	c.Check(mockableString, Equals, "bar")
	restore()
	c.Check(mockableString, Equals, "foo")

	restore = testutil.Mock(&mockableFunc, func() string { return "bar" })
	c.Check(mockableFunc(), Equals, "bar")
	restore()
	c.Check(mockableFunc(), Equals, "foo")

	restore = testutil.Mock(&mockableStruct, Foo{Bar: 12})
	c.Check(mockableStruct, Equals, Foo{Bar: 12})
	restore()
	c.Check(mockableStruct, Equals, Foo{Bar: 9})

	// XXX does not build because the types don't match

	// restore = testutil.Mock(&mockableFunc, func() int { return 555 })
	// restore()
}
