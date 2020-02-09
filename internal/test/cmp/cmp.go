package cmp

import (
	"reflect"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// Equal checks if v1 and v2 are equal with go-cmp.
func Equal(v1, v2 interface{}) bool {
	return cmp.Equal(v1, v2, cmpopts.EquateErrors(), cmp.Exporter(func(r reflect.Type) bool {
		return true
	}))
}

// Diff returns a human readable diff between v1 and v2
func Diff(v1, v2 interface{}) string {
	return cmp.Diff(v1, v2, cmpopts.EquateErrors(), cmp.Exporter(func(r reflect.Type) bool {
		return true
	}))
}
