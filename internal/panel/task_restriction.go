package panel

import "github.com/linguo2625469/workbuddy2api-panel/internal/auth"

// Separate account identity from an unverified task-compatibility marker.
// Keep the existing execution restriction without claiming that the upstream
// rejected a request, or asking the user to delete working credentials.
func growthAutomationBlockReason(a *auth.Auth) string {
	if a.IsEnterprise() {
		return "该账号带有企业账号标识（enterpriseId），本项目不支持为其执行个人成长自动任务。"
	}
	if a.HasEnterpriseSwitchSource() {
		return "当前账号未标记为企业版。登录凭据包含 token_source=enterprise_switch，但该标记不代表企业账号；本项目尚未确认这类凭据的自动成长任务兼容性，暂不执行自动任务。请在官方客户端完成任务后刷新进度，无需删除账号。"
	}
	return ""
}
