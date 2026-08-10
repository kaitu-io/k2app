//go:build go1.25

package self_test

import tls "github.com/metacubex/utls"

func getCurveID(connState tls.ConnectionState) tls.CurveID {
	return connState.CurveID
}
