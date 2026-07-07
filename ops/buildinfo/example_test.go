package buildinfo_test

import (
	"fmt"

	"github.com/dmitrymomot/forge/ops/buildinfo"
)

// Example demonstrates the "version (commit build_time)" shape produced by
// String. Read().String() is not asserted here — its output varies with the
// build environment — so this uses a constructed Info for a deterministic
// Output.
func Example() {
	info := buildinfo.Info{Version: "1.2.3", Commit: "abc1234"}
	fmt.Println(info.String())
	// Output:
	// 1.2.3 (abc1234)
}
