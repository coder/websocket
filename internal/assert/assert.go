package assert

import (
	"strings"
	"testing"
)

func Equal(t testing.TB, exp, act interface{}, name string) {
	t.Helper()
	diff := cmpDiff(exp, act)
	if diff != "" {
		t.Fatalf("unexpected %v: %v", name, diff)
	}
}

func NotEqual(t testing.TB, exp, act interface{}, name string) {
	t.Helper()
	if cmpDiff(exp, act) == "" {
		t.Fatalf("expected different %v: %+v", name, act)
	}
}

func Success(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
	}
}

func Error(t testing.TB, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
}

func ErrorContains(t testing.TB, err error, sub string) {
	t.Helper()
	Error(t, err)
	errs := err.Error()
	if !strings.Contains(errs, sub) {
		t.Fatalf("error string %q does not contain %q", errs, sub)
	}
}
