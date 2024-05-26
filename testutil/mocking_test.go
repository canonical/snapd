package testutil_test

import (
	"fmt"

	"github.com/snapcore/snapd/testutil"
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
	restore := testutil.Backup(&mockableFunc, &mockableNumber, &mockableString, &mockableStruct)
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
