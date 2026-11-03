package panel

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/linguo2625469/workbuddy2api-panel/internal/auth"
	"github.com/linguo2625469/workbuddy2api-panel/internal/pool"
	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

type acceptanceFake struct {
	before, after                     map[string][]upstream.Task
	posts                             map[string][][]string
	reads                             map[string]int
	failList, failAfterPost, failPost map[string]bool
}

func newAcceptancePanel(t *testing.T) (*Panel, *acceptanceFake) {
	t.Helper()
	f := &acceptanceFake{
		before: map[string][]upstream.Task{}, after: map[string][]upstream.Task{},
		posts: map[string][][]string{}, reads: map[string]int{},
		failList: map[string]bool{}, failAfterPost: map[string]bool{}, failPost: map[string]bool{},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		domain := "default"
		if r.Header.Get("X-Client-Platform") == "miniprogram" {
			domain = "mp"
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/activity/growth/tasks/accept":
			var body struct {
				Codes []string `json:"task_codes"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			f.posts[domain] = append(f.posts[domain], body.Codes)
			if f.failPost[domain] {
				w.WriteHeader(503)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/v2/activity/growth/tasks":
			f.reads[domain]++
			if f.failList[domain] || (f.failAfterPost[domain] && len(f.posts[domain]) > 0) {
				w.WriteHeader(503)
				return
			}
			tasks := f.before[domain]
			if len(f.posts[domain]) > 0 {
				tasks = f.after[domain]
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"tasks": tasks}})
		default:
			t.Errorf("unexpected upstream action: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(srv.Close)
	accounts := pool.New("")
	t.Cleanup(accounts.Close)
	marker := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"token_source":"enterprise_switch"}`)) + ".signature"
	accounts.Add(&auth.Auth{UID: "personal", AccessToken: marker})
	p := New(Config{APIKey: "test-key", Pool: accounts, Upstream: &upstream.Client{
		HTTP: srv.Client(), ChatBaseCN: srv.URL, BillingBaseCN: srv.URL, WebBaseCN: srv.URL,
	}})
	return p, f
}

func acceptRequest(t *testing.T, p *Panel, action, body string) (int, taskAcceptanceResult) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/panel/api/accounts/personal/tasks/"+action, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	var result taskAcceptanceResult
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return rec.Code, result
}

func TestAcceptTaskRequiresReadback(t *testing.T) {
	for _, tc := range []struct {
		name, status          string
		failRead, failPost    bool
		accepted, unconfirmed int
		httpStatus            int
	}{
		{"confirmed", "accepted", false, false, 1, 0, 200},
		{"already in progress", "in_progress", false, false, 1, 0, 200},
		{"unchanged despite OK", "not_accepted", false, false, 0, 1, 200},
		{"missing from readback", "", false, false, 0, 1, 200},
		{"readback unavailable", "accepted", true, false, 0, 1, 200},
		{"submission failed", "accepted", false, true, 0, 0, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, f := newAcceptancePanel(t)
			f.failAfterPost["default"], f.failPost["default"] = tc.failRead, tc.failPost
			if tc.status != "" {
				f.after["default"] = []upstream.Task{{TaskCode: "task-a", AcceptStatus: tc.status}}
			}
			code, result := acceptRequest(t, p, "accept", `{"task_codes":["task-a","task-a"," "]}`)
			if code != tc.httpStatus || result.Accepted != tc.accepted || len(result.Unconfirmed) != tc.unconfirmed {
				t.Fatalf("status=%d result=%+v", code, result)
			}
			if !reflect.DeepEqual(f.posts["default"], [][]string{{"task-a"}}) {
				t.Fatalf("duplicate/retried submission: %v", f.posts)
			}
			if tc.httpStatus == 200 && result.Verified != (tc.accepted == 1) {
				t.Fatalf("incorrect verification result: %+v", result)
			}
			if tc.failRead && len(result.VerificationErrors) == 0 {
				t.Fatal("readback failure must be visible")
			}
		})
	}
}

func TestAcceptAllSkipsProgressAndDeduplicatesDomains(t *testing.T) {
	p, f := newAcceptancePanel(t)
	f.before["default"] = []upstream.Task{
		{TaskCode: "shared", AcceptStatus: "not_accepted"}, {TaskCode: "shared", AcceptStatus: "not_accepted"},
		{TaskCode: "working", AcceptStatus: "in_progress"}, {TaskCode: "done", AcceptStatus: "completed"},
		{TaskCode: "locked", Locked: true}, {TaskCode: "claimed", AcceptStatus: "claimed"},
	}
	f.after["default"] = []upstream.Task{{TaskCode: "shared", AcceptStatus: "not_accepted"}}
	f.before["mp"] = append(append([]upstream.Task{}, f.before["default"]...), upstream.Task{TaskCode: "mp-only", AcceptStatus: "not_accepted"})
	f.after["mp"] = []upstream.Task{{TaskCode: "mp-only", AcceptStatus: "accepted"}}
	code, result := acceptRequest(t, p, "accept_all", "")
	if code != 200 || result.Accepted != 1 || result.Verified || !reflect.DeepEqual(result.Unconfirmed, []string{"shared"}) {
		t.Fatalf("result=%+v status=%d", result, code)
	}
	if !reflect.DeepEqual(f.posts["default"], [][]string{{"shared"}}) || !reflect.DeepEqual(f.posts["mp"], [][]string{{"mp-only"}}) {
		t.Fatalf("wrong submissions: %v", f.posts)
	}
	if f.reads["default"] != 2 || f.reads["mp"] != 2 {
		t.Fatalf("unexpected reads/retries: %v", f.reads)
	}
}

func TestAcceptAllDoesNotReportSuccessWhenSecondaryListFails(t *testing.T) {
	p, f := newAcceptancePanel(t)
	f.before["default"] = []upstream.Task{{TaskCode: "working", AcceptStatus: "in_progress"}}
	f.failList["mp"] = true
	_, result := acceptRequest(t, p, "accept_all", "")
	if result.Verified || len(result.VerificationErrors) != 1 || len(f.posts) != 0 {
		t.Fatalf("must report incomplete state: %+v posts=%v", result, f.posts)
	}
}

func TestAcceptAllNoPrimaryCandidatesStillChecksSecondaryList(t *testing.T) {
	p, f := newAcceptancePanel(t)
	f.before["default"] = []upstream.Task{{TaskCode: "working", AcceptStatus: "in_progress"}}
	f.before["mp"] = []upstream.Task{{TaskCode: "mp-only", AcceptStatus: "not_accepted"}}
	f.after["mp"] = []upstream.Task{{TaskCode: "mp-only", AcceptStatus: "accepted"}}
	_, result := acceptRequest(t, p, "accept_all", "")
	if !result.Verified || result.Accepted != 1 || len(f.posts["default"]) != 0 {
		t.Fatalf("result=%+v posts=%v", result, f.posts)
	}
}

func TestTaskListExposesIdentitySeparatelyFromAutomation(t *testing.T) {
	p, _ := newAcceptancePanel(t)
	req := httptest.NewRequest(http.MethodGet, "/panel/api/accounts/personal/tasks", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	var result struct {
		Enterprise  bool   `json:"enterprise_account"`
		BlockReason string `json:"automation_block_reason"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || result.Enterprise || !strings.Contains(result.BlockReason, "未标记为企业版") {
		t.Fatalf("incorrect identity/capability: %+v", result)
	}
}
