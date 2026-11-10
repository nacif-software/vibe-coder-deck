package platform

import (
	"fmt"
	"testing"
)

func TestHeldKeyRecordsOneKeydownUntilItsKeyup(t *testing.T) {
	h := heldKeys{}
	var kept []string
	for _, e := range [][2]string{
		{"keydown", "a"}, {"keydown", "a"}, {"keydown", "a"}, // auto-repeat while held
		{"keydown", "shift"}, {"keydown", "a"},
		{"keyup", "a"}, {"keyup", "shift"},
		{"keydown", "a"}, {"keyup", "a"}, // a new press after the release
	} {
		if !h.repeat(e[0], e[1]) {
			kept = append(kept, e[0]+" "+e[1])
		}
	}
	want := "[keydown a keydown shift keyup a keyup shift keydown a keyup a]"
	if got := fmt.Sprint(kept); got != want {
		t.Fatalf("kept %s, want %s", got, want)
	}
}
