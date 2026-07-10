package qrcode_test

import (
	"fmt"

	"github.com/dmitrymomot/forge/core/qrcode"
)

func ExampleEncode() {
	m, err := qrcode.Encode("https://forge.example")
	if err != nil {
		panic(err)
	}
	fmt.Println(m.Size() > 0)
	// Output: true
}
