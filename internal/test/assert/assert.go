package assert

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Equal asserts exp == act.
func Equal(t testing.TB, name string, exp, got interface{}) {
	t.Helper()

	if !reflect.DeepEqual(exp, got) {
		t.Fatalf("unexpected %v: expected %#v but got %#v", name, exp, got)
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

	s := fmt.Sprint(v)
	if !strings.Contains(s, sub) {
		t.Fatalf("expected %q to contain %q", s, sub)
	}
}

// ErrorIs asserts errors.Is(got, exp)
func ErrorIs(t testing.TB, exp, got error) {
	t.Helper()

	if !errors.Is(got, exp) {
		t.Fatalf("expected %v but got %v", exp, got)
	}
}
