package appid

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/datakaveri/dx-common-go/auth"
	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// Middleware returns middleware that authenticates machine clients sending
// `Authorization: Basic base64(appId:appSecret)` via the controlplane's
// VerifyAppId gRPC. Requests without a Basic header fall through to next
// untouched (typically the JWT middleware), so the two schemes compose.
func Middleware(client *Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			appID, secret, ok := basicCredentials(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			user, err := client.VerifyAppId(r.Context(), appID, secret)
			if err != nil {
				var verr *VerificationError
				if errors.As(err, &verr) {
					dxerrors.WriteError(w, dxerrors.NewUnauthorized("app credentials rejected: "+verr.Code))
					return
				}
				dxerrors.WriteError(w, dxerrors.NewInternal("app credential verification unavailable"))
				return
			}

			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), user)))
		})
	}
}

// basicCredentials extracts appId/appSecret from a Basic Authorization header.
func basicCredentials(r *http.Request) (string, string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", "", false
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "basic") {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(parts[1]))
	if err != nil {
		return "", "", false
	}
	id, secret, found := strings.Cut(string(decoded), ":")
	if !found || id == "" || secret == "" {
		return "", "", false
	}
	return id, secret, true
}
