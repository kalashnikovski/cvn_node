package node

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// The desktop wallet's screens call the program's functions by name. The
// window framework builds that name from the Go type, as "package.Type"; the
// screens' side is written out in frontend/wailsjs. If the two disagree every
// button in the wallet silently does nothing, so they are compared here.
func TestWalletScreensCallTheProgramByItsRealName(t *testing.T) {
	goName := reflect.TypeOf(&App{}).Elem().String() // what the framework uses
	if goName != "node.App" {
		t.Fatalf("the wallet type is now named %q; the screens' link must be regenerated to match", goName)
	}
	link, err := os.ReadFile("../frontend/wailsjs/wailsjs/go/node/App.js")
	if err != nil {
		t.Fatalf("the screens' link file is missing: %v", err)
	}
	for _, method := range []string{"GetLocalNodeTelemetry", "FetchWalletBalanceQuery", "BroadcastLocalTransactionSubmit", "CombineCoinsSubmit"} {
		if _, exists := reflect.TypeOf(&App{}).MethodByName(method); !exists {
			t.Errorf("the program no longer has the wallet function %s", method)
		}
		if !strings.Contains(string(link), "window['go']['node']['App']['"+method+"']") {
			t.Errorf("the screens do not call %s under the name the program gives it", method)
		}
	}
}
