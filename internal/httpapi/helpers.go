package httpapi

import (
	"net/http"
	"time"

	"xiaozhang/internal/auth"
	"xiaozhang/internal/ids"
)

func sessionOf(r *http.Request) *auth.Session { return auth.SessionFrom(r.Context()) }

// timeNow returns current time; tests can replace via build hooks later.
var timeNow = time.Now

func newOpID() string { return ids.New() }
