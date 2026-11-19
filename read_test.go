//go:build !js

package websocket

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

func TestReadMessageCompletion(t *testing.T) {
	t.Parallel()

	for _, encoding := range []struct {
		name    string
		payload []byte
		flate   bool
	}{
		{"plain", []byte("Hello"), false},
		// RFC 7692 sections 7.2.3.1 and 7.2.3.4 encode the same message.
		{"deflate", []byte{0xf2, 0x48, 0xcd, 0xc9, 0xc9, 0x07, 0x00}, true},
		{"deflateBFINAL", []byte{0xf3, 0x48, 0xcd, 0xc9, 0xc9, 0x07, 0x00, 0x00}, true},
	} {
		for _, client := range []bool{false, true} {
			for _, reader := range []bool{false, true} {
				name := fmt.Sprintf("%s/client=%t/reader=%t", encoding.name, client, reader)
				t.Run(name, func(t *testing.T) {
					payload := encoding.payload
					first := byte(opText)
					if encoding.flate {
						first |= 0x40
					}
					frame := func(op byte, length int, data []byte) []byte {
						b := []byte{op, byte(length)}
						if !client {
							b[1] |= 0x80
							b = append(b, 0, 0, 0, 0)
						}
						return append(b, data...)
					}
					split := len(payload) - 1
					fragment := func(last []byte) []byte {
						return append(frame(first, split, payload[:split]), last...)
					}

					for _, tc := range []struct {
						name     string
						wire     []byte
						complete bool
					}{
						{"missingContinuation", frame(first, len(payload), payload), false},
						{"partialContinuationHeader", fragment([]byte{0x80}), false},
						{"missingPayload", frame(first|0x80, len(payload), nil), false},
						{"partialPayload", frame(first|0x80, len(payload), payload[:split]), false},
						{"missingContinuationPayload", fragment(frame(0x80, 1, nil)), false},
						{"partialContinuationPayload", fragment(frame(0x80, 2, payload[split:])), false},
						{"complete", frame(first|0x80, len(payload), payload), true},
						{"completeFragmented", fragment(frame(0x80, 1, payload[split:])), true},
						{"emptyFinalFragment", append(frame(first, len(payload), payload), frame(0x80, 0, nil)...), true},
					} {
						t.Run(tc.name, func(t *testing.T) {
							t.Parallel()
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()
							local, peer := net.Pipe()
							c := newConn(connConfig{
								rwc: local, client: client,
								br: bufio.NewReader(local), bw: bufio.NewWriter(local),
							})
							if encoding.flate {
								c.copts = CompressionNoContextTakeover.opts()
							}
							defer c.CloseNow()
							defer peer.Close()

							wire := tc.wire
							if tc.complete {
								wire = append(wire, frame(0x80|byte(opBinary), 4, []byte("next"))...)
								wire = append(wire, frame(first|0x80, len(payload), payload)...)
							}
							written := make(chan error, 1)
							go func() {
								_, err := peer.Write(wire)
								peer.Close()
								written <- err
							}()

							read := func() (MessageType, []byte, error) {
								if !reader {
									return c.Read(ctx)
								}
								typ, r, err := c.Reader(ctx)
								if err != nil {
									return typ, nil, err
								}
								b, err := io.ReadAll(r)
								return typ, b, err
							}
							typ, b, err := read()
							if !tc.complete {
								if !errors.Is(err, io.ErrUnexpectedEOF) {
									t.Fatalf("read truncated message %q: got %v, want unexpected EOF", b, err)
								}
							} else {
								if err != nil || typ != MessageText || string(b) != "Hello" {
									t.Fatalf("read complete message: got %v %q %v", typ, b, err)
								}
								typ, b, err = read()
								if err != nil || typ != MessageBinary || string(b) != "next" {
									t.Fatalf("read following message: got %v %q %v", typ, b, err)
								}
								typ, b, err = read()
								if err != nil || typ != MessageText || string(b) != "Hello" {
									t.Fatalf("read repeated message: got %v %q %v", typ, b, err)
								}
							}
							select {
							case err := <-written:
								if err != nil {
									t.Fatal(err)
								}
							case <-ctx.Done():
								t.Fatal(ctx.Err())
							}
						})
					}
				})
			}
		}
	}
}
