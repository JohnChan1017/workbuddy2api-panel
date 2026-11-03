package panel

import (
	"fmt"
	"strings"
	"time"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// A successful submission is not evidence that the task state changed.
type taskAcceptanceResult struct {
	OK                 bool     `json:"ok"`
	Verified           bool     `json:"verified"`
	Accepted           int      `json:"accepted"`
	Failed             []string `json:"failed"`
	Unconfirmed        []string `json:"unconfirmed"`
	VerificationErrors []string `json:"verification_errors"`
	Message            string   `json:"message"`
}

func taskIsAccepted(t upstream.Task) bool {
	if t.Claimed {
		return true
	}
	switch t.AcceptStatus {
	case "accepted", "in_progress", "completed", "claimed":
		return true
	}
	return false
}

func uniqueTaskCodes(codes []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code != "" && !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	return out
}

// Read back once without resubmitting. Missing, unchanged, or unreadable states
// are unconfirmed, not success and not proof that the upstream rejected them.
func (result *taskAcceptanceResult) verify(codes []string, list func() ([]upstream.Task, error)) {
	if len(codes) == 0 {
		return
	}
	tasks, err := list()
	if err != nil {
		result.Unconfirmed = append(result.Unconfirmed, codes...)
		result.VerificationErrors = append(result.VerificationErrors, "接受请求已提交，但任务状态回读失败")
		return
	}
	confirmed := map[string]bool{}
	for _, task := range tasks {
		confirmed[task.TaskCode] = confirmed[task.TaskCode] || taskIsAccepted(task)
	}
	for _, code := range codes {
		if confirmed[code] {
			result.Accepted++
		} else {
			result.Unconfirmed = append(result.Unconfirmed, code)
		}
	}
}

func (result *taskAcceptanceResult) submitBatches(codes []string, accept func([]string) error, list func() ([]upstream.Task, error)) {
	var submitted []string
	for start := 0; start < len(codes); start += 20 {
		batch := codes[start:min(start+20, len(codes))]
		if err := accept(batch); err != nil {
			result.Failed = append(result.Failed, batch...)
		} else {
			submitted = append(submitted, batch...)
		}
		if start+20 < len(codes) {
			time.Sleep(acceptBatchGap)
		}
	}
	result.verify(submitted, list)
}

func (result *taskAcceptanceResult) finish() {
	result.OK = true // The API completed; Verified describes the task outcome.
	result.Verified = len(result.Failed) == 0 && len(result.Unconfirmed) == 0 && len(result.VerificationErrors) == 0
	if !result.Verified {
		result.Message = fmt.Sprintf("已确认接受 %d 项，未确认 %d 项，提交失败 %d 项。请刷新任务状态后再操作。", result.Accepted, len(result.Unconfirmed), len(result.Failed))
		if len(result.VerificationErrors) > 0 {
			result.Message += "部分任务状态查询失败，结果不完整。"
		}
	} else if result.Accepted == 0 {
		result.Message = "当前没有可接受的新任务（已接受、进行中、已完成或未解锁的任务均跳过）"
	} else {
		result.Message = fmt.Sprintf("已确认接受 %d 个任务", result.Accepted)
	}
}
