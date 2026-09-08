package redfish

import (
	"context"
	"fmt"
	"net/http"
)

// ---------- 会话认证（手册 3.74–3.76） ----------

// SessionService 会话服务资源（3.74）
type SessionService struct {
	ODataContext   string `json:"@odata.context"`
	ODataID        string `json:"@odata.id"`
	ODataType      string `json:"@odata.type"`
	ID             string `json:"Id"`
	Name           string `json:"Name"`
	ServiceEnabled *bool  `json:"ServiceEnabled"`
	SessionTimeout *int   `json:"SessionTimeout"`
	Sessions       Link   `json:"Sessions"`
}

// Session 会话实例（3.76）
type Session struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ODataType    string `json:"@odata.type"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	UserName     string `json:"UserName"`
}

// Login 建立会话认证：POST /redfish/v1/SessionService/Sessions（NoAuth，手册 3.75.2），
// 成功后客户端改用 X-Auth-Token 携带凭证，后续请求不再传 Basic。
// BMC 侧会话数量有限（SessionService 可查），长生命周期进程用完须 Logout。
func (c *Client) Login(ctx context.Context) (*Session, error) {
	body := map[string]string{
		"UserName": c.username,
		"Password": c.password,
	}
	// 会话创建响应头携带 X-Auth-Token 与 Location，需要拿到响应头
	u := c.host + "/redfish/v1/SessionService/Sessions"
	bodyReader, err := jsonReader(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("redfish: 构造登录请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("redfish: 登录请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := readAll(resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusCreated {
		return nil, newHTTPError(resp.StatusCode, "/redfish/v1/SessionService/Sessions", data)
	}

	var s Session
	if err := decodeJSON(data, &s); err != nil {
		return nil, fmt.Errorf("redfish: 解析会话响应失败: %w", err)
	}
	token := resp.Header.Get("X-Auth-Token")
	if token == "" {
		return nil, fmt.Errorf("redfish: 登录成功但响应缺少 X-Auth-Token 头")
	}
	uri := resp.Header.Get("Location")
	if uri == "" {
		uri = s.ODataID
	}
	c.mu.Lock()
	c.sessionToken = token
	c.sessionURI = uri
	c.mu.Unlock()
	return &s, nil
}

// Logout 注销当前会话（DELETE 会话资源，204）。无会话时为空操作。
// 无论 DELETE 成败本地凭证均清除，客户端回退 Basic 认证。
// DELETE 在清 token 之前发出：session-only 部署下该请求必须携带 X-Auth-Token。
func (c *Client) Logout(ctx context.Context) error {
	c.mu.RLock()
	uri := c.sessionURI
	c.mu.RUnlock()
	if uri == "" {
		return nil
	}
	err := c.Delete(ctx, uri)
	c.mu.Lock()
	c.sessionToken = ""
	c.sessionURI = ""
	c.mu.Unlock()
	return err
}

// SessionService 获取会话服务设置
func (c *Client) SessionService(ctx context.Context) (*SessionService, error) {
	var ss SessionService
	if err := c.Get(ctx, "/redfish/v1/SessionService", &ss); err != nil {
		return nil, err
	}
	return &ss, nil
}

// UpdateSessionService 更新会话服务设置（ServiceEnabled/SessionTimeout，204）
func (c *Client) UpdateSessionService(ctx context.Context, enabled *bool, timeoutSeconds *int) error {
	body := map[string]any{}
	if enabled != nil {
		body["ServiceEnabled"] = *enabled
	}
	if timeoutSeconds != nil {
		body["SessionTimeout"] = *timeoutSeconds
	}
	return c.Patch(ctx, "/redfish/v1/SessionService", body, nil)
}

// Sessions 列出当前全部会话
func (c *Client) Sessions(ctx context.Context) ([]Session, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/SessionService/Sessions")
	if err != nil {
		return nil, err
	}
	out := make([]Session, 0, len(coll.Members))
	for _, m := range coll.Members {
		var s Session
		if err := c.Get(ctx, m.ODataID, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// DeleteSession 删除指定会话（强制注销某会话，ConfigureManager 权限）
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	return c.Delete(ctx, "/redfish/v1/SessionService/Sessions/"+sessionID)
}
