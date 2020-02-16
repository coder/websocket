package cmp

import (
	"reflect"

	"github.com/golang/protobuf/proto"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// Diff returns a human readable diff between v1 and v2
func Diff(v1, v2 interface{}) string {
	return cmp.Diff(v1, v2, cmpopts.EquateErrors(), cmp.Exporter(func(r reflect.Type) bool {
		return true
	}), cmp.Comparer(proto.Equal))
}
