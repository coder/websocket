package assert

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// https://github.com/google/go-cmp/issues/40#issuecomment-328615283
func cmpDiff(exp, act interface{}) string {
	return cmp.Diff(exp, act, deepAllowUnexported(exp, act))
}

func deepAllowUnexported(vs ...interface{}) cmp.Option {
	m := make(map[reflect.Type]struct{})
	for _, v := range vs {
		structTypes(reflect.ValueOf(v), m)
	}
	var typs []interface{}
	for t := range m {
		typs = append(typs, reflect.New(t).Elem().Interface())
	}
	return cmp.AllowUnexported(typs...)
}

func structTypes(v reflect.Value, m map[reflect.Type]struct{}) {
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Ptr:
		if !v.IsNil() {
			structTypes(v.Elem(), m)
		}
	case reflect.Interface:
		if !v.IsNil() {
			structTypes(v.Elem(), m)
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			structTypes(v.Index(i), m)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			structTypes(v.MapIndex(k), m)
		}
	case reflect.Struct:
		m[v.Type()] = struct{}{}
		for i := 0; i < v.NumField(); i++ {
			structTypes(v.Field(i), m)
		}
	}
}

func Equalf(t *testing.T, exp, act interface{}, f string, v ...interface{}) {
	t.Helper()
	diff := cmpDiff(exp, act)
	if diff != "" {
		t.Fatalf(f+": %v", append(v, diff)...)
	}
}

func Success(t *testing.T, err error) {
	t.Helper()
	Equalf(t, error(nil), err, "unexpected failure")
}
