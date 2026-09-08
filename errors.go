package redfish

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// HTTPError 非 2xx 响应错误，携带 Redfish 标准 error 载荷信息（手册 2.3/Table 1）。
// 通过 errors.As 判断状态码，例如：
//
//	var he *redfish.HTTPError
//	if errors.As(err, &he) && he.StatusCode == 401 { ... }
type HTTPError struct {
	StatusCode int
	Path       string
	// Redfish error 对象字段（存在时）
	Message  string
	Extended []ExtendedInfo
	// ExtendedInvalid 为 true 表示载荷携带 @Message.ExtendedInfo 但解析失败（信息降级，主错误不丢）
	ExtendedInvalid bool
}

// ExtendedInfo Redfish error.@Message.ExtendedInfo 数组元素
type ExtendedInfo struct {
	ODataType   string   `json:"@odata.type"`
	MessageID   string   `json:"MessageId"`
	Message     string   `json:"Message"`
	Severity    string   `json:"Severity"`
	MessageArgs []string `json:"MessageArgs"`
	Resolution  string   `json:"Resolution"`
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		msg := fmt.Sprintf("redfish: %s -> HTTP %d: %s", e.Path, e.StatusCode, e.Message)
		for _, x := range e.Extended {
			msg += fmt.Sprintf("; %s: %s", x.MessageID, x.Message)
		}
		if e.ExtendedInvalid {
			msg += "; 扩展信息载荷解析失败"
		}
		return msg
	}
	if e.ExtendedInvalid {
		return fmt.Sprintf("redfish: %s -> HTTP %d（扩展信息载荷解析失败）", e.Path, e.StatusCode)
	}
	return fmt.Sprintf("redfish: %s -> HTTP %d", e.Path, e.StatusCode)
}

// newHTTPError 解析 Redfish 错误响应体 {"error":{"code":..,"message":..,"@Message.ExtendedInfo":[..]}}
func newHTTPError(statusCode int, path string, data []byte) *HTTPError {
	he := &HTTPError{StatusCode: statusCode, Path: path}
	var payload struct {
		Error struct {
			Message    string          `json:"message"`
			ExtendedRW json.RawMessage `json:"@Message.ExtendedInfo"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &payload); err == nil {
		he.Message = payload.Error.Message
		if len(payload.Error.ExtendedRW) > 0 && string(payload.Error.ExtendedRW) != "null" {
			// 兼容数组与单对象两种形态；解析失败显式标记，不静默丢弃
			if payload.Error.ExtendedRW[0] == '[' {
				if err := json.Unmarshal(payload.Error.ExtendedRW, &he.Extended); err != nil {
					he.ExtendedInvalid = true
				}
			} else {
				var one ExtendedInfo
				if err := json.Unmarshal(payload.Error.ExtendedRW, &one); err != nil {
					he.ExtendedInvalid = true
				} else {
					he.Extended = []ExtendedInfo{one}
				}
			}
		}
	}
	return he
}

// IsNotFound 是否 404
func IsNotFound(err error) bool { return statusIs(err, http.StatusNotFound) }

// IsUnauthorized 是否 401（会话过期/密码错误）
func IsUnauthorized(err error) bool { return statusIs(err, http.StatusUnauthorized) }

// IsForbidden 是否 403（权限不足，手册 2.6：操作所需 Privilege 未满足）
func IsForbidden(err error) bool { return statusIs(err, http.StatusForbidden) }

// statusIs 沿 errors.Unwrap 链查找 HTTPError（库内部普遍用 %w 包装，裸类型断言会漏）
func statusIs(err error, code int) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.StatusCode == code
}
