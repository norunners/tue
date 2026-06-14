//go:build !js || !wasm

package tue

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestMountReportsUnsupportedPlatform(t *testing.T) {
	component := CompOf(&initFixture{}, func(*initFixture) VNode {
		return Text("value")
	})

	_, err := Mount("#app", component)

	if err == nil || err.Error() != "mount is only supported under js/wasm" {
		t.Errorf("mismatch Mount error (-expected, +actual):\n%s", cmp.Diff("mount is only supported under js/wasm", errorString(err)))
	}
}
