package wsutil

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

func VerifyOrigin(w http.ResponseWriter, r *http.Request, hosts ...string) error {
	if len(hosts) == 0 {
		return errors.New("must provide at least one host to wsutil.VerifyOrigin")
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return nil
	}

	url, err := url.Parse(origin)
	if err != nil {
		http.Error(w, "invalid origin header", http.StatusBadRequest)
		return fmt.Errorf("invalid origin header %q: %v", origin, err)
	}
	for _, h := range hosts {
		if h == url.Host {
			return nil
		}
	}

	http.Error(w, "unexpected origin host", http.StatusForbidden)
	return fmt.Errorf("unexpected origin host in %q: %v", origin, err)
}
