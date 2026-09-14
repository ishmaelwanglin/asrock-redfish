package redfish

// 本文件实现 AMI MegaRAC SP-X 的**私有 Web REST** 客户端（不是 Redfish）。
//
// # 为什么需要它
//
// AST2600 / MegaRAC SP-X 上「命名时区」（如 Asia/Shanghai）在 Redfish 侧没有可用入口：
// DMTF 标准的 Manager.TimeZoneName 自 Manager v1_10_0 才存在，而实机
// /redfish/v1/$metadata 只声明到 Manager.v1_6_0，PATCH TimeZoneName 实测返回
// 400 + Base.1.x.PropertyUnknown。标准里可写的 DateTimeLocalOffset 只能表达 UTC 偏移，
// 固件会把时区槽写成 GMT+08 这类「手动偏移」值——同一块固件里时区只有**一个字符串槽**，
// 要么存 IANA 名、要么存手动偏移，所以偏移写成功恰恰等于命名时区被清空。
// 唯一能落命名时区的通道，是 BMC 自带 HTML5 页面用的那条私有 REST：
//
//	POST   /api/session                表单 username/password → JSON（含 CSRFToken），种 QSESSIONID
//	GET    /api/settings/date-time     整个配置对象（含只读的 zones 候选表）
//	PUT    /api/settings/date-time     全量回写
//	DELETE /api/settings/date-time     注销会话
//
// 该路径只注册了 GET/PUT/DELETE，**没有 POST**（实测 POST → 404 {"error":"Invalid API Call","code":1010}，
// PUT → 401 打到鉴权层）。除 X-CSRFTOKEN 头外还需带同名 cookie（页面写作 garc），缺一被 web 层拒。
// 本 API **不接受 Basic 认证**（401 响应不带 WWW-Authenticate，硬塞 Basic 头仍回 cc=7）。
//
// # 未验证项（禁止当作事实使用）
//
//   - 登录口对**非浏览器客户端**是否放行：实测本机 curl 的十余种请求形态（HTTP/1.0/1.1/h2、
//     UA、Referer、Origin、Accept、X-Requested-With、CSRF 配对、Sec-Fetch-*、无 Content-Type、
//     multipart、body 尺寸）打 POST /api/session **一律 501 Not Implemented**，而同机同期
//     POST /api/actions/power 与 /api/sol/session 能正常到达应用层（401）。成因未查明。
//     本客户端把原始状态码与响应体片段原样带进错误，便于当场判定，不做任何"大概能行"的假设。
//   - utc_minutes 的符号约定（东为正还是西为正）未实机确认，因此 SetTimeZone **默认不写该字段**；
//     需要改时区偏移时由调用方显式传值。
//   - PUT 请求体是否容忍缺字段（例如剥掉只读的 zones 以省带宽）未实测，故一律全量回送，
//     与页面行为保持一致——页面那条全量 PUT 是已观测可用的。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// 私有 Web REST 的固定路径与头名（均取自 BMC 侧 SPA 原文，非猜测）
const (
	webSessionPath  = "/api/session"
	webDateTimePath = "/api/settings/date-time"
	webCSRFHeader   = "X-CSRFTOKEN"
	webCSRFCookie   = "garc"
	// webAccept 照抄 jQuery dataType:"json" 发出的 Accept，尽量贴住浏览器特征
	webAccept = "application/json, text/javascript, */*; q=0.01"
)

// 登录环节的可分辨失败（都对应 SPA 里真实存在的分支，不是自造语义）
var (
	// ErrNotLoggedIn 未登录就调用需要会话的方法。登录是有副作用的动作，不隐式代做。
	ErrNotLoggedIn = errors.New("asrock-redfish: 尚未登录，请先调用 Login")
	// ErrTFARequired 该账号开了双因子，Web 通道必须走 /api/verify_otp，本客户端不支持。
	ErrTFARequired = errors.New("asrock-redfish: 该账号启用了双因子认证（TFA），私有 Web API 无法自动登录")
	// ErrPasswordChangeRequired 首次登录/口令过期，固件强制改密，必须人工在页面处理。
	ErrPasswordChangeRequired = errors.New("asrock-redfish: 固件要求先修改口令才能使用 Web 会话")
	// ErrNoTimezoneField date-time 资源里没有 timezone 字段：说明该机型不是 SP-X 这套私有 API，
	// 继续 PUT 等于拿猜想的 schema 做全量替换，因此直接拒绝并且**不发任何写请求**。
	ErrNoTimezoneField = errors.New("asrock-redfish: /api/settings/date-time 未返回 timezone 字段，判定本机不支持该改法，已放弃写入")
)

// webTzNameRe 允许的时区取值字符：既放行 IANA 名（Asia/Shanghai、Etc/GMT-8），也放行页面下拉里的
// 手动偏移值（GMT+08）。集合刻意不含引号、花括号、空白与控制符，避免把任意字符串塞进 JSON 值。
var webTzNameRe = regexp.MustCompile(`^[A-Za-z0-9_+/\-.]{1,64}$`)

// webTitleRe 从 lighttpd 的 HTML 错误页里抽原因（501/411 这类是 web 层直接产出的，不是 JSON）
var webTitleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// WebError 私有 Web REST 的非 2xx 错误。
//
// 这条通道有两套与 Redfish 不同的错误体，必须分开表达，否则分类会退化成永不命中的死码
// （参见 ops 仓 buglist BUG-20260914-02 的同型事故）：
//
//	{"cc":7,"error":"Invalid Authentication"}            IPMI completion code，鉴权层
//	{"error":"Invalid API Call","code":1010}             私有 API 路由层
//	<html><title>501 - Not Implemented</title>…          lighttpd 自己拦下，压根没进应用
type WebError struct {
	StatusCode int
	Path       string
	// CC 是 IPMI completion code，CCHas 表示响应里确实带了该字段（cc=0 也是合法值）
	CC    int
	CCHas bool
	// Code 是私有 API 自身的错误码（如 1010 Invalid API Call）
	Code  int
	CodeS bool
	// Message 人类可读原因；HTML 错误页取 <title>，无法识别的载荷取截断后的原文
	Message string
}

func (e *WebError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "asrock-redfish: web %s -> HTTP %d", e.Path, e.StatusCode)
	if e.CCHas {
		fmt.Fprintf(&b, " [cc=%d]", e.CC)
	}
	if e.CodeS {
		fmt.Fprintf(&b, " [code=%d]", e.Code)
	}
	if e.Message != "" {
		b.WriteString(": " + e.Message)
	}
	return b.String()
}

// IsUnauthorized 是否鉴权失败（401，或 IPMI cc=7 Invalid Authentication）
func (e *WebError) IsUnauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized || (e.CCHas && e.CC == 7)
}

// IsUnsupported 是否"本机没有这条路"：404 路由不存在、501 web 层不支持该方法、405 方法不允许。
// 调用方应据此决定降级，而不是把错误当成功吞掉。
func (e *WebError) IsUnsupported() bool {
	switch e.StatusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented:
		return true
	}
	return false
}

// newWebError 解析私有 API 的错误载荷。三种形态都要照顾到，且**不静默丢弃**任何可读信息。
func newWebError(statusCode int, path string, data []byte) *WebError {
	we := &WebError{StatusCode: statusCode, Path: path}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var payload struct {
			CC    *int            `json:"cc"`
			Code  *int            `json:"code"`
			Error json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(trimmed, &payload); err == nil {
			we.CC, we.CCHas = derefInt(payload.CC)
			we.Code, we.CodeS = derefInt(payload.Code)
			// error 字段在私有 API 里是字符串，在 Redfish 里是对象；两种都认
			if len(payload.Error) > 0 {
				if payload.Error[0] == '"' {
					var s string
					if json.Unmarshal(payload.Error, &s) == nil {
						we.Message = s
					}
				} else {
					var o struct {
						Message string `json:"message"`
					}
					if json.Unmarshal(payload.Error, &o) == nil && o.Message != "" {
						we.Message = o.Message
					}
				}
			}
		}
	}
	if we.Message == "" {
		if m := webTitleRe.FindSubmatch(trimmed); len(m) == 2 {
			we.Message = strings.TrimSpace(string(m[1]))
		} else if len(trimmed) > 0 {
			we.Message = webSnippet(trimmed, 160)
		}
	}
	return we
}

func derefInt(p *int) (int, bool) {
	if p == nil {
		return 0, false
	}
	return *p, true
}

// webSnippet 按 **rune** 截断并清洗成合法 UTF-8（按字节切会把中文劈成半个字符）
func webSnippet(b []byte, max int) string {
	r := []rune(strings.ToValidUTF8(string(b), "\uFFFD"))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max]) + "…"
}

// WebClient 是 BMC 私有 Web REST 客户端。零值不可用，须通过 NewWebClient 创建。
//
// 与 Redfish 的 Client 相互独立：认证靠 QSESSIONID 会话 cookie + X-CSRFTOKEN 头，
// 不使用 Basic。会话状态由内部互斥锁保护，可跨 goroutine 共享。
//
// 用法（读-改-写，全程不自己造时间戳）：
//
//	w := redfish.NewWebClient("192.168.48.134", user, pass, redfish.WithInsecureTLS(true))
//	if ok, err := w.SupportsDateTime(ctx); err == nil && ok {
//	    if err := w.Login(ctx); err == nil {
//	        defer func() { _ = w.Logout(ctx) }()   // 会话槽有限，用完立刻还
//	        res, err := w.SetTimeZone(ctx, "Asia/Shanghai", nil)
//	    }
//	}
type WebClient struct {
	host     string // 归一化后形如 https://192.168.48.134
	username string
	password string
	http     *http.Client

	mu     sync.Mutex
	csrf   string
	logged bool
}

// NewWebClient 创建私有 API 客户端。host 支持 "ip"、"ip:port"、"https://ip" 三种写法。
//
// 复用 Redfish 侧的 Option（WithTimeout / WithInsecureTLS / WithHTTPClient），
// 因此超时与自签证书的处理与 Client 完全一致。注意：本客户端必须保存会话 cookie，
// 会**无条件设置**底层 http.Client 的 Jar；若你传入自带 Jar 的 client，会被替换。
func NewWebClient(host, username, password string, opts ...Option) *WebClient {
	base := NewClient(host, username, password, opts...) // 复用 host 归一化与 Transport 设定
	jar, _ := cookiejar.New(nil)                         // nil 选项下不会失败
	base.http.Jar = jar
	return &WebClient{
		host:     base.host,
		username: username,
		password: password,
		http:     base.http,
	}
}

// Host 返回归一化后的服务地址
func (w *WebClient) Host() string { return w.host }

// LoggedIn 是否已持有会话
func (w *WebClient) LoggedIn() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.logged
}

// do 执行一次私有 API 请求，返回状态码与原始响应体；非 2xx 转成 *WebError。
// 未登录时 csrf 为空，天然不会带上 X-CSRFTOKEN，探针因此是真正匿名的。
func (w *WebClient) do(ctx context.Context, method, path string, body []byte, contentType string) (int, []byte, error) {
	if !strings.HasPrefix(path, "/") {
		return 0, nil, fmt.Errorf("asrock-redfish: 路径必须以 / 开头: %s", path)
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, w.host+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("asrock-redfish: 构造请求失败: %w", err)
	}
	req.Header.Set("Accept", webAccept)
	// 贴住浏览器特征。实测对登录口的 501 无效（本文件头部已记为未验证），但对
	// 部分按 UA/Referer 取舍的 web 层有意义，留着不亏。
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", w.host+"/")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	w.mu.Lock()
	csrf := w.csrf
	w.mu.Unlock()
	if csrf != "" {
		req.Header.Set(webCSRFHeader, csrf)
	}

	resp, err := w.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("asrock-redfish: 请求 %s %s 失败: %w", method, path, err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("asrock-redfish: 读取 %s %s 响应失败: %w", method, path, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, data, nil
	}
	return resp.StatusCode, data, newWebError(resp.StatusCode, path, data)
}

// webSessionReply 登录响应的已知字段。字段名与 SPA 里读到的名字逐一对应。
type webSessionReply struct {
	OK             *int   `json:"ok"`
	Code           *int   `json:"code"`
	CSRFToken      string `json:"CSRFToken"`
	Privilege      string `json:"privilege"`
	PrivilegeID    *int   `json:"privilege_id"`
	UserID         *int   `json:"user_id"`
	Channel        *int   `json:"channel"`
	ServerAddr     string `json:"server_addr"`
	PasswordStatus *int   `json:"passwordStatus"`
	TFAEnabled     *int   `json:"TFAEnabled"`
	TFAStatus      *int   `json:"TFAStatus"`
}

// Login 建立 Web 会话：POST /api/session（表单），取 CSRFToken，种 QSESSIONID 与 garc。
//
// 语义来自 SPA 原文：`ok === 0` 才是成功（**注意 0 是成功**）；
// `ok != 0 && passwordStatus == 1` 是强制改密；`TFAEnabled == 1 && TFAStatus != 1` 需要 OTP。
func (w *WebClient) Login(ctx context.Context) error {
	form := url.Values{"username": {w.username}, "password": {w.password}}
	status, data, err := w.do(ctx, http.MethodPost, webSessionPath,
		[]byte(form.Encode()), "application/x-www-form-urlencoded")
	if err != nil {
		// 501 之类由 web 层直接拦下的情形，WebError 已带上 HTML <title>，原样交给调用方
		return fmt.Errorf("asrock-redfish: 登录失败: %w", err)
	}
	var reply webSessionReply
	if err := json.Unmarshal(bytes.TrimSpace(data), &reply); err != nil {
		return fmt.Errorf("asrock-redfish: 登录响应不是 JSON（HTTP %d，疑似被重定向到登录页或被 web 层拦截）: %s",
			status, webSnippet(data, 200))
	}
	if reply.TFAEnabled != nil && *reply.TFAEnabled == 1 && (reply.TFAStatus == nil || *reply.TFAStatus != 1) {
		return ErrTFARequired
	}
	if reply.OK == nil {
		return fmt.Errorf("asrock-redfish: 登录响应缺少 ok 字段（HTTP %d）: %s", status, webSnippet(data, 200))
	}
	if *reply.OK != 0 {
		if reply.PasswordStatus != nil && *reply.PasswordStatus == 1 {
			return ErrPasswordChangeRequired
		}
		suffix := ""
		if reply.Code != nil {
			suffix = fmt.Sprintf("，code=%d（会话数已满或凭据无效）", *reply.Code)
		}
		return fmt.Errorf("asrock-redfish: 登录被拒绝 ok=%d%s", *reply.OK, suffix)
	}
	if reply.CSRFToken == "" {
		return fmt.Errorf("asrock-redfish: 登录成功但未返回 CSRFToken，后续请求无法通过校验")
	}

	// 页面在登录后把 CSRF token 也写成 cookie（garc），web 层可能按 cookie/头配对校验，照做。
	// 页面另有一条 refresh_disable=1，那是给 SPA 关掉自动刷新的，对程序化客户端无意义，不复刻。
	if u, perr := url.Parse(w.host); perr == nil && w.http.Jar != nil {
		w.http.Jar.SetCookies(u, []*http.Cookie{{
			Name:     webCSRFCookie,
			Value:    reply.CSRFToken,
			Path:     "/",
			Secure:   true,
			SameSite: http.SameSiteLaxMode,
		}})
	}
	w.mu.Lock()
	w.csrf, w.logged = reply.CSRFToken, true
	w.mu.Unlock()
	return nil
}

// Logout 注销会话（DELETE /api/session）。会话已失效（401/cc=7）视为幂等成功，
// 其余错误照实返回——批量调用要靠它归还有限的会话槽，不能静默失败。
func (w *WebClient) Logout(ctx context.Context) error {
	w.mu.Lock()
	logged := w.logged
	w.mu.Unlock()
	if !logged {
		return nil
	}
	_, _, err := w.do(ctx, http.MethodDelete, webSessionPath, nil, "")
	var we *WebError
	if err != nil && errors.As(err, &we) && we.IsUnauthorized() {
		err = nil // 已经不在了，目标状态达成
	}
	w.mu.Lock()
	w.csrf, w.logged = "", false
	w.mu.Unlock()
	return err
}

// SupportsDateTime **免认证**探测本机是否提供 /api/settings/date-time。
//
// 判据是实测出来的三态：401 = 路由存在且需要会话（可用）；404/code=1010 = 没有这条路由；
// 501 = web 层不支持该方法。用于在 Redfish TimeZoneName 被拒之后、动用登录之前先做一次
// 零副作用的分流，避免对不支持的机型反复尝试登录。
func (w *WebClient) SupportsDateTime(ctx context.Context) (bool, error) {
	_, _, err := w.do(ctx, http.MethodGet, webDateTimePath, nil, "")
	if err == nil {
		return true, nil // 罕见：允许匿名读，同样说明资源在
	}
	var we *WebError
	if errors.As(err, &we) {
		switch {
		case we.StatusCode == http.StatusUnauthorized:
			return true, nil
		case we.IsUnsupported():
			return false, nil
		}
	}
	return false, err
}

// DateTimeSettings /api/settings/date-time 的响应。
//
// 原始载荷按 **原始字节** 保留（map[string]json.RawMessage），这样 timestamp 之类的数字不会
// 经 float64 往返而变形——PUT 是全量替换语义，回写的每一个字节都必须是读到的那个字节。
type DateTimeSettings struct {
	raw map[string]json.RawMessage
}

// Raw 返回内部原始载荷。返回值与内部状态共享，只用于读；要改动请走 SetTimeZone。
func (d *DateTimeSettings) Raw() map[string]json.RawMessage { return d.raw }

// Get 取某个字段的原始 JSON
func (d *DateTimeSettings) Get(key string) (json.RawMessage, bool) {
	v, ok := d.raw[key]
	return v, ok
}

// Has 是否存在某字段
func (d *DateTimeSettings) Has(key string) bool {
	_, ok := d.raw[key]
	return ok
}

// Keys 返回全部字段名（已排序，便于断言与日志）
func (d *DateTimeSettings) Keys() []string {
	ks := make([]string, 0, len(d.raw))
	for k := range d.raw {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// Timezone 当前 timezone 字段（缺失或类型不符返回空串）
func (d *DateTimeSettings) Timezone() string {
	raw, ok := d.raw["timezone"]
	if !ok {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}
	return s
}

// UTCTotalMinutes 当前 utc_minutes 字段；缺失或类型不符返回 nil（调用方据此决定要不要动它）
func (d *DateTimeSettings) UTCTotalMinutes() *int {
	raw, ok := d.raw["utc_minutes"]
	if !ok {
		return nil
	}
	var n int
	if json.Unmarshal(raw, &n) != nil {
		return nil
	}
	return &n
}

// Summary 单行摘要，适合进日志（不含凭据）
func (d *DateTimeSettings) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "timezone=%q", d.Timezone())
	if v := d.UTCTotalMinutes(); v != nil {
		fmt.Fprintf(&b, " utc_minutes=%d", *v)
	} else {
		b.WriteString(" utc_minutes=<absent>")
	}
	if raw, ok := d.raw["timestamp"]; ok {
		fmt.Fprintf(&b, " timestamp=%s", webSnippet(raw, 40))
	}
	return b.String()
}

// MarshalJSON 原样序列化读到的对象（全量回送的基座）
func (d *DateTimeSettings) MarshalJSON() ([]byte, error) {
	if d.raw == nil {
		return []byte("null"), nil
	}
	return json.Marshal(d.raw)
}

// GetDateTime 读取日期时间/NTP/PTP 配置对象。需要已登录会话。
func (w *WebClient) GetDateTime(ctx context.Context) (*DateTimeSettings, error) {
	if !w.LoggedIn() {
		return nil, ErrNotLoggedIn
	}
	status, data, err := w.do(ctx, http.MethodGet, webDateTimePath, nil, "")
	if err != nil {
		return nil, fmt.Errorf("asrock-redfish: 读取 date-time 失败: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(data), &raw); err != nil {
		return nil, fmt.Errorf("asrock-redfish: date-time 响应不是 JSON 对象（HTTP %d）: %s",
			status, webSnippet(data, 200))
	}
	return &DateTimeSettings{raw: raw}, nil
}

// TimeZoneApply 一次时区写入的结果。**Applied 才是判据，err == nil 不代表生效**。
type TimeZoneApply struct {
	Requested string // 我们要求写入的时区
	Before    string // 写入前固件里的值
	After     string // 写入后回读到的值
	// Applied 表示回读的 timezone 与 Requested 一致。false 时必须当作失败：
	// 有些固件会接受 PUT 却不采纳该字段（静默吞写），只有回读能拆穿。
	Applied bool
	// UTCMinutesBefore/After 是 utc_minutes 写入前后的值；nil 表示该字段不存在或未参与本次写入
	UTCMinutesBefore *int
	UTCMinutesAfter  *int
	// ChangedKeys 本次真正改动的字段名，便于审计
	ChangedKeys []string
	// Note 诊断信息（未生效时给出下一步线索）
	Note string
}

// SetTimeZone 把命名时区写成 tzName（例如 "Asia/Shanghai"；下拉里的 "GMT+08" 这类手动偏移也接受）。
//
// 实现是严格的读-改-写：先 GET，只替换 timezone（utcTotalMinutes 非 nil 时才连带改 utc_minutes），
// 其余字段连同只读的 zones **原样 PUT 回去**，再 GET 回读比对。这样页面能做到的事我们逐字节对齐，
// 不会因为"只发一个字段"撞上未验证的部分更新语义。
//
// utcTotalMinutes 传 nil 表示**不动** utc_minutes——推荐做法：只让命名时区落地、且目标时区偏移
// 与当前一致时（例如 GMT+08 → Asia/Shanghai），保留固件自己算出来的那份最安全。
// 需要显式设置时可用 UTCTotalMinutesFor 取候选值，但注意其符号约定尚未实机确认。
func (w *WebClient) SetTimeZone(ctx context.Context, tzName string, utcTotalMinutes *int) (*TimeZoneApply, error) {
	if !w.LoggedIn() {
		return nil, ErrNotLoggedIn
	}
	if !webTzNameRe.MatchString(tzName) {
		return nil, fmt.Errorf("asrock-redfish: 时区名不合法（只允许 IANA 名或 GMT±hh 形式，且不得含引号/花括号/空白）: %q", tzName)
	}

	cur, err := w.GetDateTime(ctx)
	if err != nil {
		return nil, err
	}
	res := &TimeZoneApply{
		Requested:        tzName,
		Before:           cur.Timezone(),
		UTCMinutesBefore: cur.UTCTotalMinutes(),
	}
	if !cur.Has("timezone") {
		// schema 与预期不符。此时 PUT 等于拿猜想的结构做全量替换，可能覆掉别人写过的字段，
		// 因此一个写请求都不发。
		return res, ErrNoTimezoneField
	}

	payload := make(map[string]json.RawMessage, len(cur.raw)+2)
	for k, v := range cur.raw {
		payload[k] = v
	}
	tzBytes, err := json.Marshal(tzName)
	if err != nil {
		return nil, fmt.Errorf("asrock-redfish: 序列化时区名失败: %w", err)
	}
	payload["timezone"] = tzBytes
	res.ChangedKeys = append(res.ChangedKeys, "timezone")
	if utcTotalMinutes != nil {
		minBytes, err := json.Marshal(*utcTotalMinutes)
		if err != nil {
			return nil, fmt.Errorf("asrock-redfish: 序列化 utc_minutes 失败: %w", err)
		}
		payload["utc_minutes"] = minBytes
		res.ChangedKeys = append(res.ChangedKeys, "utc_minutes")
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return res, fmt.Errorf("asrock-redfish: 序列化回写载荷失败: %w", err)
	}
	if _, _, err := w.do(ctx, http.MethodPut, webDateTimePath, body, "application/json"); err != nil {
		res.Note = "PUT 被拒绝，未做任何变更判定"
		return res, fmt.Errorf("asrock-redfish: 写入时区失败: %w", err)
	}

	after, err := w.GetDateTime(ctx)
	if err != nil {
		res.Note = "写入请求已被接受，但回读失败，无法判定是否生效"
		return res, err
	}
	res.After = after.Timezone()
	res.UTCMinutesAfter = after.UTCTotalMinutes()
	res.Applied = res.After == tzName
	if !res.Applied {
		res.Note = fmt.Sprintf("固件接受了 PUT 但回读的 timezone 仍是 %q（静默吞写）；命名时区在该固件上不可用，应回退到上层降级策略", res.After)
	}
	return res, nil
}

// UTCTotalMinutesFor 给出某时区**当下**的 UTC 偏移分钟数，约定**东为正**（Asia/Shanghai → 480）。
//
// 注意：私有 API 里 utc_minutes 的真实符号约定**未实机验证**，本函数只提供候选值，
// 是否传给 SetTimeZone 由调用方决定。若本机 zoneinfo 缺失，导入 `_ "time/tzdata"` 或改由调用方给值。
func UTCTotalMinutesFor(tzName string) (int, error) {
	if !webTzNameRe.MatchString(tzName) {
		return 0, fmt.Errorf("asrock-redfish: 时区名不合法: %q", tzName)
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return 0, fmt.Errorf("asrock-redfish: 本机找不到时区 %q 的时区库（可导入 _ \"time/tzdata\"）: %w", tzName, err)
	}
	_, offset := time.Now().In(loc).Zone()
	return offset / 60, nil
}
