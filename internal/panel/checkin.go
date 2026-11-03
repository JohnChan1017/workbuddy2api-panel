package panel

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/linguo2625469/workbuddy2api-panel/internal/upstream"
)

// checkinErrorMessage avoids displaying gateway HTML as a check-in result.
// An authorization rejection alone proves neither expiration nor an account ban.
func checkinErrorMessage(err error) (string, int) {
	var ue *upstream.Error
	if errors.As(err, &ue) {
		if ue.Status == http.StatusUnauthorized || ue.Status == http.StatusForbidden {
			return fmt.Sprintf("上游鉴权拒绝（HTTP %d）。请在官方客户端确认账号状态，必要时重新授权；此错误不能单独判断为凭据过期或封号。", ue.Status), ue.Status
		}
		body := strings.ToLower(strings.TrimSpace(ue.Msg))
		if strings.Contains(body, "<html") || strings.Contains(body, "<!doctype") {
			return fmt.Sprintf("上游返回错误页面（HTTP %d），本次操作未完成。", ue.Status), ue.Status
		}
		return err.Error(), ue.Status
	}
	return err.Error(), 0
}
