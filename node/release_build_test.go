//go:build !multinode && !devtools

package node

import "testing"

// An ordinary build, which is what a release is, holds no test network and
// no test tool.
func TestAnOrdinaryBuildHasNoTestNetworkOrTools(t *testing.T) {
	if len(extraNetworks) != 0 {
		t.Errorf("an ordinary build knows %d extra network(s)", len(extraNetworks))
	}
	if devToolCommand != nil {
		t.Error("an ordinary build contains a test tool")
	}
	saved := Params
	defer func() { Params = saved; applyParams() }()
	if err := SelectNetwork("localnet"); err == nil {
		t.Error("an ordinary build accepted the local test network")
	}
}
