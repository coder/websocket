package websocket_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"reflect"

	"github.com/google/go-cmp/cmp"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
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

func assertEqualf(exp, act interface{}, f string, v ...interface{}) error {
	if diff := cmpDiff(exp, act); diff != "" {
		return fmt.Errorf(f+": %v", append(v, diff)...)
	}
	return nil
}

func assertJSONEcho(ctx context.Context, c *websocket.Conn, n int) error {
	exp := randString(n)
	err := wsjson.Write(ctx, c, exp)
	if err != nil {
		return err
	}

	var act interface{}
	err = wsjson.Read(ctx, c, &act)
	if err != nil {
		return err
	}

	return assertEqualf(exp, act, "unexpected JSON")
}

func assertJSONRead(ctx context.Context, c *websocket.Conn, exp interface{}) error {
	var act interface{}
	err := wsjson.Read(ctx, c, &act)
	if err != nil {
		return err
	}

	return assertEqualf(exp, act, "unexpected JSON")
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func randString(n int) string {
	return hex.EncodeToString(randBytes(n))[:n]
}

func assertEcho(ctx context.Context, c *websocket.Conn, typ websocket.MessageType, n int) error {
	p := randBytes(n)
	err := c.Write(ctx, typ, p)
	if err != nil {
		return err
	}
	typ2, p2, err := c.Read(ctx)
	if err != nil {
		return err
	}
	err = assertEqualf(typ, typ2, "unexpected data type")
	if err != nil {
		return err
	}
	return assertEqualf(p, p2, "unexpected payload")
}

func assertSubprotocol(c *websocket.Conn, exp string) error {
	return assertEqualf(exp, c.Subprotocol(), "unexpected subprotocol")
}
