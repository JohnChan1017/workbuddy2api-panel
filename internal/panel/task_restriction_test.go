package panel

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

func TestGrowthRestrictionDistinguishesAccountAndTokenSource(t *testing.T) {
	token := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"token_source":"enterprise_switch"}`)) + ".signature"
	for _, tc := range []struct {
		name, enterpriseID, token, want, absent string
	}{
		{"personal source marker", "", token, "当前账号未标记为企业版", "该账号带有企业账号标识"},
		{"enterprise account", "enterprise-test", "opaque", "该账号带有企业账号标识", "token_source=enterprise_switch"},
		{"enterprise with source marker", "enterprise-test", token, "该账号带有企业账号标识", "当前账号未标记为企业版"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()
			accounts := pool.New("")
			defer accounts.Close()
			a := &auth.Auth{UID: "test-account", EnterpriseID: tc.enterpriseID, AccessToken: tc.token}
			accounts.Add(a)
			p := New(Config{APIKey: "test-key", Pool: accounts, Upstream: &upstream.Client{
				HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL, WebBaseCN: srv.URL,
			}})
			for _, action := range []string{"auto", "auto_all"} {
				req := httptest.NewRequest(http.MethodPost, "/panel/api/accounts/test-account/tasks/"+action, strings.NewReader(`{"task_code":"chat_5"}`))
				req.Header.Set("Authorization", "Bearer test-key")
				rec := httptest.NewRecorder()
				p.ServeHTTP(rec, req)
				if rec.Code != http.StatusUnprocessableEntity {
					t.Fatalf("%s: status=%d want 422", action, rec.Code)
				}
				var body struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(body.Error, tc.want) || strings.Contains(body.Error, tc.absent) {
					t.Fatalf("incorrect account diagnosis: %q", body.Error)
				}
				for _, misleading := range []string{"请移除", "重新登录", "会被上游忽略", "无个人成长体系"} {
					if strings.Contains(body.Error, misleading) {
						t.Errorf("unsupported instruction/assertion: %q", misleading)
					}
				}
			}
			if calls.Load() != 0 {
				t.Fatalf("restricted requests must not reach upstream: calls=%d", calls.Load())
			}
			if a.IsEnterprise() != (tc.enterpriseID != "") {
				t.Fatal("token-source marker must not change account identity")
			}
		})
	}
}

func TestGrowthRestrictionUnmarkedPersonalAccount(t *testing.T) {
	a := &auth.Auth{AccessToken: "opaque-personal-token"}
	if got := growthAutomationBlockReason(a); got != "" {
		t.Fatalf("unmarked personal account must not receive an enterprise diagnosis: %q", got)
	}
}
