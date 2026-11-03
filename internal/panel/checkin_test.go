package panel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

func TestAccountCheckinReportsActualOutcome(t *testing.T) {
	for _, tc := range []struct {
		name, body, status string
		httpStatus         int
		done, enterprise   bool
		balanceFails       bool
	}{
		{name: "success", httpStatus: 200, body: `{"code":0,"data":{}}`, status: "done", done: true},
		{name: "already", httpStatus: 200, body: `{"code":14001,"msg":"今日已签到"}`, status: "already_done", done: true},
		{name: "unauthorized_html", httpStatus: 401, body: `<html><title>401 Authorization Required</title>openresty apisix</html>`, status: "failed"},
		{name: "unauthorized_already_text", httpStatus: 401, body: `{"msg":"already denied"}`, status: "failed"},
		{name: "forbidden_html", httpStatus: 403, body: `<html>Forbidden</html>`, status: "failed", balanceFails: true},
		{name: "business_failure", httpStatus: 200, body: `{"code":10001,"msg":"操作失败"}`, status: "failed"},
		{name: "success_balance_failed", httpStatus: 200, body: `{"code":0,"data":{}}`, status: "done", done: true, balanceFails: true},
		{name: "enterprise_skipped", status: "skipped", enterprise: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkinCalls, balanceCalls := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("unexpected method: %s", r.Method)
				}
				switch r.URL.Path {
				case "/v2/billing/meter/daily-checkin":
					checkinCalls++
					w.WriteHeader(tc.httpStatus)
					w.Write([]byte(tc.body))
				case "/v2/billing/meter/get-user-resource", "/v2/billing/meter/get-enterprise-user-usage":
					balanceCalls++
					if tc.balanceFails {
						w.WriteHeader(401)
						w.Write([]byte(`<html>401 Authorization Required</html>`))
					} else if tc.enterprise {
						w.Write([]byte(`{"code":0,"data":{"credit":2,"limitNum":10}}`))
					} else {
						w.Write([]byte(`{"code":0,"data":{"Response":{"Data":{"Accounts":[{"CapacitySize":10,"CapacityRemain":8}]}}}}`))
					}
				default:
					t.Errorf("unexpected upstream request: %s", r.URL.Path)
					w.WriteHeader(400)
				}
			}))
			defer srv.Close()
			accounts := pool.New("")
			defer accounts.Close()
			a := &auth.Auth{UID: "personal", AccessToken: "test-token"}
			if tc.enterprise {
				a.EnterpriseID = "test-enterprise"
			}
			accounts.Add(a)
			p := New(Config{APIKey: "test-key", Pool: accounts, Upstream: &upstream.Client{
				HTTP: srv.Client(), BillingBaseCN: srv.URL,
			}})
			req := httptest.NewRequest(http.MethodPost, "/panel/api/accounts/personal/checkin", nil)
			req.Header.Set("Authorization", "Bearer test-key")
			rec := httptest.NewRecorder()
			p.ServeHTTP(rec, req)
			// Upstream auth failures must not become a panel API-key 401.
			if rec.Code != http.StatusOK {
				t.Fatalf("HTTP status = %d, body=%s", rec.Code, rec.Body.String())
			}
			var result struct {
				OK             bool   `json:"ok"`
				Done           bool   `json:"checkin_done"`
				Status         string `json:"checkin_status"`
				UpstreamStatus int    `json:"checkin_upstream_status"`
				Message        string `json:"checkin_message"`
				BalanceError   string `json:"balance_error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Done != tc.done || result.Status != tc.status || result.OK != (tc.done || tc.enterprise) {
				t.Fatalf("unexpected result: %+v", result)
			}
			state, _ := accounts.Status(a.UID)
			if state.CheckinDone != tc.done {
				t.Fatalf("pool checkin state = %v, want %v", state.CheckinDone, tc.done)
			}
			wantCheckinCalls := 1
			if tc.enterprise {
				wantCheckinCalls = 0
			}
			if checkinCalls != wantCheckinCalls || balanceCalls != 1 {
				t.Fatalf("requests: checkin=%d balance=%d", checkinCalls, balanceCalls)
			}
			if strings.Contains(strings.ToLower(rec.Body.String()), "<html") || strings.Contains(rec.Body.String(), "openresty") {
				t.Fatalf("raw error page leaked into result: %s", rec.Body.String())
			}
			if tc.httpStatus == 401 || tc.httpStatus == 403 {
				if result.UpstreamStatus != tc.httpStatus || !strings.Contains(result.Message, "上游鉴权拒绝") {
					t.Fatalf("missing authorization diagnostic: %+v", result)
				}
			}
			if tc.balanceFails && !strings.Contains(result.BalanceError, "HTTP 401") {
				t.Fatalf("missing balance failure: %+v", result)
			}
		})
	}
}
