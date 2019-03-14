package ws

import (
	"net/url"
)

func Origins(origins ...string) func(origin *url.URL) bool {
	return func(origin *url.URL) bool {
		for _, h := range origins {
			if origin.Host == h {
				return true
			}
		}

		return false
	}
}
