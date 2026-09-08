// Package redfish 提供 ASPEED AST2600 BMC（AMI 固件，Redfish v1.11）的全功能客户端。
// 依据《BMC (AST2600) Generic Redfish v1.11 API User Guide Rev 1.00》(2023-12) 实现，
// 覆盖手册 3.2–3.126 全部资源与附录 FixedBootOrder。
//
// 基础用法：
//
//	c, err := redfish.NewClient("192.168.1.100", "admin", "password", redfish.WithInsecureTLS(true))
//	sys, err := c.System(ctx, "") // ID 留空自动发现（AST2600 为 "Self"）
//	err = c.SystemReset(ctx, "", redfish.ResetTypeForceRestart)
//
// BMC 普遍使用自签名证书，无法通过系统信任链校验时须显式传 WithInsecureTLS(true)；
// 也可通过 WithHTTPClient 注入带自签 CA 的定制客户端。
package redfish

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// 默认请求超时。BMC 响应普遍偏慢（尤其清单类资源），给足余量。
const defaultTimeout = 30 * time.Second

// Client Redfish 客户端。零值不可用，须通过 NewClient 创建。
// 默认 Basic 认证；调用 Login 切换为会话认证（批量操作推荐，避免每请求传密码）。
// 会话状态由内部互斥锁保护，可在多 goroutine 间共享同一 Client。
type Client struct {
	host     string // 形如 "https://192.168.1.100:443"，无 scheme 时默认 https
	username string
	password string
	http     *http.Client

	mu           sync.RWMutex
	sessionToken string // 非空表示会话认证，请求携带 X-Auth-Token（受 mu 保护）
	sessionURI   string // 会话资源 URI，Logout 用（受 mu 保护）
}

// Option NewClient 的可选配置
type Option func(*Client)

// WithTimeout 覆盖默认 30s 超时
func WithTimeout(d time.Duration) Option { return func(c *Client) { c.http.Timeout = d } }

// WithInsecureTLS 跳过 TLS 证书校验（BMC 自签证书场景由调用方显式声明）
func WithInsecureTLS(insecure bool) Option {
	return func(c *Client) {
		if tr, ok := c.http.Transport.(*http.Transport); ok {
			tr.TLSClientConfig.InsecureSkipVerify = insecure
		}
	}
}

// WithHTTPClient 完全替换底层 http.Client（需自行设置 Transport 与超时）。
// 本选项会丢弃此前 WithTimeout/WithInsecureTLS 的效果，须放在最后使用。
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.http = hc } }

// NewClient 创建客户端。host 支持 "ip"、"ip:port"、"https://ip" 三种写法。
func NewClient(host, username, password string, opts ...Option) *Client {
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	c := &Client{
		host:     strings.TrimRight(host, "/"),
		username: username,
		password: password,
		http: &http.Client{
			Timeout: defaultTimeout,
			// BMC 侧 HTTP/2 支持不稳，强制 HTTP/1.1
			Transport: &http.Transport{
				TLSClientConfig:       &tls.Config{}, // 预置空配置：WithInsecureTLS 直接写入此对象
				TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
				MaxIdleConnsPerHost:   4,
				IdleConnTimeout:       60 * time.Second,
				ResponseHeaderTimeout: 25 * time.Second,
			},
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Host 返回归一化后的服务地址
func (c *Client) Host() string { return c.host }

// jsonReader 构造 JSON 请求体（Login 需要自持 Request，无法复用 do）
func jsonReader(v any) (io.Reader, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("redfish: 序列化请求体失败: %w", err)
	}
	return bytes.NewReader(data), nil
}

func readAll(resp *http.Response) ([]byte, error) {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("redfish: 读取响应失败: %w", err)
	}
	return data, nil
}

func decodeJSON(data []byte, out any) error { return json.Unmarshal(data, out) }

// ---------- 底层请求 ----------

type apiRequest struct {
	method string
	path   string
	body   any
	// multipart 字段（UpdateService 上传用）；非空时 body 忽略
	multipart []multipartField
	// outLocation 非 nil 时，成功响应（含 202）的 Location 头写入其中（PATCH 产生任务时用）
	outLocation *string
	// ifMatch 非空时请求携带 If-Match 头（428 Precondition Required 的标准解法）
	ifMatch string
	// outETag 非 nil 时，成功响应的 ETag 头写入其中
	outETag *string
}

type multipartField struct {
	name        string
	filename    string
	contentType string
	data        []byte
}

// do 执行请求并解析响应。out 非 nil 时把 2xx body 反序列化进 out。
// 返回 (statusCode, error)。204/201/202 均视为成功。
func (c *Client) do(ctx context.Context, r apiRequest, out any) (int, error) {
	u := c.host + r.path
	if !strings.HasPrefix(r.path, "/") {
		return 0, fmt.Errorf("redfish: 路径必须以 / 开头: %s", r.path)
	}

	var bodyReader io.Reader
	var contentType string
	if len(r.multipart) > 0 {
		var buf bytes.Buffer
		boundary := fmt.Sprintf("redfish%s", strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", ""))
		for _, f := range r.multipart {
			fmt.Fprintf(&buf, "--%s\r\nContent-Disposition: form-data; name=%q; filename=%q\r\n", boundary, f.name, f.filename)
			ct := f.contentType
			if ct == "" {
				ct = "application/octet-stream"
			}
			fmt.Fprintf(&buf, "Content-Type: %s\r\n\r\n", ct)
			buf.Write(f.data)
			buf.WriteString("\r\n")
		}
		fmt.Fprintf(&buf, "--%s--\r\n", boundary)
		bodyReader = &buf
		contentType = "multipart/form-data; boundary=" + boundary
	} else if r.body != nil {
		data, err := json.Marshal(r.body)
		if err != nil {
			return 0, fmt.Errorf("redfish: 序列化请求体失败: %w", err)
		}
		bodyReader = bytes.NewReader(data)
		contentType = "application/json"
	}

	req, err := http.NewRequestWithContext(ctx, r.method, u, bodyReader)
	if err != nil {
		return 0, fmt.Errorf("redfish: 构造请求失败: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	if r.ifMatch != "" {
		req.Header.Set("If-Match", r.ifMatch)
	}
	c.mu.RLock()
	token := c.sessionToken
	c.mu.RUnlock()
	if token != "" {
		req.Header.Set("X-Auth-Token", token)
	} else {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("redfish: 请求 %s %s 失败: %w", r.method, r.path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, fmt.Errorf("redfish: 读取响应失败: %w", err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if r.outLocation != nil {
			*r.outLocation = resp.Header.Get("Location")
		}
		if r.outETag != nil {
			*r.outETag = resp.Header.Get("ETag")
		}
		if out != nil && len(data) > 0 {
			if err := json.Unmarshal(data, out); err != nil {
				return resp.StatusCode, fmt.Errorf("redfish: 解析 %s 响应失败: %w", r.path, err)
			}
		}
		return resp.StatusCode, nil
	}
	return resp.StatusCode, newHTTPError(resp.StatusCode, r.path, data)
}

// ---------- 便捷谓词 ----------

// Get 获取单个资源
func (c *Client) Get(ctx context.Context, path string, out any) error {
	_, err := c.do(ctx, apiRequest{method: http.MethodGet, path: path}, out)
	return err
}

// Patch 部分更新资源。204/200 均成功；out 非 nil 时接收 200 响应体。
func (c *Client) Patch(ctx context.Context, path string, body, out any) error {
	_, err := c.do(ctx, apiRequest{method: http.MethodPatch, path: path, body: body}, out)
	return err
}

// Post 创建资源或调用 Action，返回状态码与原始响应（已解析进 out）
func (c *Client) Post(ctx context.Context, path string, body, out any) (int, error) {
	return c.do(ctx, apiRequest{method: http.MethodPost, path: path, body: body}, out)
}

// Delete 删除资源（204）
func (c *Client) Delete(ctx context.Context, path string) error {
	_, err := c.do(ctx, apiRequest{method: http.MethodDelete, path: path}, nil)
	return err
}

// ---------- 集合发现（手册：不同固件资源 ID 不一致，AST2600 为 Self，其他厂商可能为 1） ----------

// CollectionMember 集合成员引用
type CollectionMember struct {
	ODataID string `json:"@odata.id"`
}

// Collection 通用集合资源
type Collection struct {
	ODataContext    string             `json:"@odata.context"`
	ODataID         string             `json:"@odata.id"`
	ODataType       string             `json:"@odata.type"`
	Name            string             `json:"Name"`
	Members         []CollectionMember `json:"Members"`
	MembersCount    int                `json:"Members@odata.count"`
	MembersNextLink string             `json:"Members@odata.nextLink"`
}

// nextLinkPath 归一化 Members@odata.nextLink（可能是绝对路径或完整 URL → path?query）
func nextLinkPath(link string) string {
	if u, err := url.Parse(link); err == nil && strings.HasPrefix(link, "http") {
		if u.RawQuery != "" {
			return u.Path + "?" + u.RawQuery
		}
		return u.Path
	}
	return link
}

// CollectionAll 获取集合并跟随 Members@odata.nextLink 翻页（手册 3.34：PCIeDevices 等大集合分页返回）
func (c *Client) CollectionAll(ctx context.Context, path string) (*Collection, error) {
	var coll Collection
	if err := c.Get(ctx, path, &coll); err != nil {
		return nil, err
	}
	for coll.MembersNextLink != "" {
		var next Collection
		if err := c.Get(ctx, nextLinkPath(coll.MembersNextLink), &next); err != nil {
			return nil, err
		}
		coll.Members = append(coll.Members, next.Members...)
		coll.MembersNextLink = next.MembersNextLink
	}
	return &coll, nil
}

// MemberIDs 返回集合全部成员的 @odata.id
func (col *Collection) MemberIDs() []string {
	ids := make([]string, 0, len(col.Members))
	for _, m := range col.Members {
		ids = append(ids, m.ODataID)
	}
	return ids
}

// PathTail 取资源 URI 的尾段 ID（/redfish/v1/Systems/Self → Self）
func PathTail(p string) string {
	parts := strings.Split(strings.TrimRight(p, "/"), "/")
	return parts[len(parts)-1]
}

// FirstMemberID 取集合第一个成员的 ID（单系统 BMC 即唯一实例）
func (c *Client) FirstMemberID(ctx context.Context, collectionPath, what string) (string, error) {
	coll, err := c.CollectionAll(ctx, collectionPath)
	if err != nil {
		return "", fmt.Errorf("发现 %s 失败: %w", what, err)
	}
	if len(coll.Members) == 0 {
		return "", fmt.Errorf("redfish: BMC 上未发现 %s", what)
	}
	return PathTail(coll.Members[0].ODataID), nil
}

// resolveID ID 为空时自动发现，否则原样返回
func (c *Client) resolveID(ctx context.Context, id, collectionPath, what string) (string, error) {
	if id != "" {
		return id, nil
	}
	return c.FirstMemberID(ctx, collectionPath, what)
}

// ResolveSystemID 归一化系统 ID（空 → 自动发现）
func (c *Client) ResolveSystemID(ctx context.Context, systemID string) (string, error) {
	return c.resolveID(ctx, systemID, "/redfish/v1/Systems", "computer system")
}

// ResolveManagerID 归一化管理器 ID（空 → 自动发现）
func (c *Client) ResolveManagerID(ctx context.Context, managerID string) (string, error) {
	return c.resolveID(ctx, managerID, "/redfish/v1/Managers", "manager")
}

// ResolveChassisID 归一化机箱 ID（空 → 自动发现）
func (c *Client) ResolveChassisID(ctx context.Context, chassisID string) (string, error) {
	return c.resolveID(ctx, chassisID, "/redfish/v1/Chassis", "chassis")
}
