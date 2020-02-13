package assert

import (
	"fmt"
	"strings"
	"testing"

	"nhooyr.io/websocket/internal/test/cmp"
)

// Equal asserts exp == act.
func Equal(t testing.TB, name string, exp, act interface{}) {
	t.Helper()

	if diff := cmp.Diff(exp, act); diff != "" {
		t.Fatalf("unexpected %v: %v", name, diff)
	}
}

// Success asserts err == nil.
func Success(t testing.TB, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

// Error asserts err != nil.
func Error(t testing.TB, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("expected error")
	}
}

// Contains asserts the fmt.Sprint(v) contains sub.
func Contains(t testing.TB, v interface{}, sub string) {
	t.Helper()

	vstr := fmt.Sprint(v)
	if !strings.Contains(vstr, sub) {
		t.Fatalf("expected %q to contain %q", vstr, sub)
	}
}
