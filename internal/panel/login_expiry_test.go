package panel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type loginTestTransport func(*http.Request) (*http.Response, error)

func (f loginTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLoginPollRejectsExpiredSessionBeforeUpstream(t *testing.T) {
	old := loginHTTP
	t.Cleanup(func() { loginHTTP = old })
	calls := 0
	loginHTTP = &http.Client{Transport: loginTestTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"code":1,"msg":"pending"}`)),
			Header:     make(http.Header),
		}, nil
	})}
	for _, tc := range []struct {
		name   string
		age    time.Duration
		exists bool
		want   int
	}{
		{"expired", loginTTL + time.Minute, true, http.StatusNotFound},
		{"at expiry", loginTTL, true, http.StatusNotFound},
		{"unknown", 0, false, http.StatusNotFound},
		{"active", time.Minute, true, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newTestPanel()
			if tc.exists {
				p.logins["test-state"] = loginSession{created: time.Now().Add(-tc.age), realm: "cn"}
			}
			before := calls
			req := httptest.NewRequest(http.MethodGet, "/panel/api/login/poll?state=test-state", nil)
			req.Header.Set("Authorization", "Bearer test-key")
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("code=%d want=%d", rec.Code, tc.want)
			}
			if tc.want == http.StatusNotFound {
				if calls != before || len(p.logins) != 0 {
					t.Fatal("expired/unknown session must not contact upstream and must be removed")
				}
			} else if calls != before+1 || len(p.logins) != 1 {
				t.Fatal("active pending session must remain available for polling")
			}
		})
	}
}
