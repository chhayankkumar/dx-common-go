package opa

import (
	"net/http"

	"github.com/datakaveri/dx-common-go/auth"
	dxerrors "github.com/datakaveri/dx-common-go/errors"
)

// Middleware returns a chi-compatible middleware that allows a request only
// when the Evaluator's policy allows {method, path, roles, org_id} for the
// already-resolved auth.DxUser in context. MUST run after
// dx-common-go/auth/resolver.Middleware (or any other populator of DxUser).
func Middleware(e *Evaluator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := auth.UserFromCtx(r.Context())
			if !ok {
				dxerrors.WriteError(w, dxerrors.NewUnauthorized("no authenticated user in context"))
				return
			}

			allowed, err := e.Allow(r.Context(), Input{
				Method: r.Method,
				Path:   r.URL.Path,
				Roles:  user.Roles,
				OrgID:  user.OrganisationID,
			})
			if err != nil {
				dxerrors.WriteError(w, dxerrors.NewInternal("policy evaluation failed"))
				return
			}
			if !allowed {
				dxerrors.WriteError(w, dxerrors.NewForbidden("not authorised by policy for "+r.Method+" "+r.URL.Path))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
