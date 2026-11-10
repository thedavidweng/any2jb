package savekit_test

import (
	"github.com/thedavidweng/any2jb/savekit"
	"testing"
)

func TestOnlyReadOperationsCanBeRequested(t *testing.T) {
	for _, action := range []string{"accounts", "titles", "inventory"} {
		if err := (savekit.Request{Action: action}).Validate(); err != nil {
			t.Fatal(err)
		}
	}
	target := savekit.Request{Action: "inspect", TitleID: "010012900d38a000", UID: "11111111111111111111111111111111"}
	for _, action := range []string{"inspect", "backup", "filetime"} {
		target.Action = action
		if err := target.Validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, action := range []string{"write", "restore", "delete", "verify", "unknown"} {
		target.Action = action
		if target.Validate() == nil {
			t.Fatalf("accepted %s", action)
		}
	}
	target.Action = "filetime"
	target.All = true
	if target.Validate() == nil {
		t.Fatal("timestamp scan accepted multiple archives")
	}
}
