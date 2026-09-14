package redfish

// 私有 Web REST 客户端的单元测试。全部对着 httptest 假 BMC 跑，不碰真机。
// 断言刻意盯在两类容易出事的地方：
//   1) 全量回写是否真的"原样"（timestamp 不能被 float 改写、未改的字段不能丢）
//   2) 失败是否被静默吞掉（PUT 被接受但 timezone 没变，必须判失败；schema 不符时一个写请求都不许发）

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

const (
	fakeWebUser = "admin"
	fakeWebPass = "secret"
	fakeCSRF    = "tok-web-123"
)

// fakeWebBMC 模仿 MegaRAC SP-X 私有 API 的关键行为：会话、CSRF 头校验、PUT 全量语义。
type fakeWebBMC struct {
	t *testing.T
	s *httptest.Server

	mu sync.Mutex
	// doc 是当前 /api/settings/date-time 的内容（原始 JSON 值）
	doc map[string]json.RawMessage
	// 行为开关
	sessionValid     bool
	loginStatus      int
	loginBody        string
	loginContentType string
	putStatus        int
	putSwallow       bool // 接受 PUT 但不采纳 timezone（静默吞写）
	deleteStatus     int
	// 观测点
	logins     int
	gets       int
	puts       int
	deletes    int
	lastPut    map[string]json.RawMessage
	lastPutRaw string
	lastGetHdr http.Header
	lastPutHdr http.Header
}

func newFakeWebBMC(t *testing.T) *fakeWebBMC {
	t.Helper()
	f := &fakeWebBMC{
		t:                t,
		sessionValid:     false,
		doc:              defaultWebDoc(),
		loginStatus:      http.StatusOK,
		loginContentType: "application/json",
		putStatus:        http.StatusOK,
		deleteStatus:     http.StatusNoContent,
	}
	f.s = httptest.NewTLSServer(f) // BMC 是 https，Secure cookie 只在 https 下会被 jar 收下
	t.Cleanup(f.s.Close)
	return f
}

// defaultWebDoc 字段集合取自 SPA 里 date_time 模型实际读到的属性集（zones 用短数组代表）
func defaultWebDoc() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"id":            json.RawMessage(`3`),
		"timezone":      json.RawMessage(`"GMT+08"`),
		"timestamp":     json.RawMessage(`1755238694.0000000001`), // 刻意带上小数尾巴，验证不被 float 往返改写
		"utc_minutes":   json.RawMessage(`-480`),
		"ntp_auto_date": json.RawMessage(`1`),
		"primary_ntp":   json.RawMessage(`"192.168.50.126"`),
		"secondary_ntp": json.RawMessage(`"time.nist.gov"`),
		"ptp_auto_date": json.RawMessage(`0`),
		"ptp_preset":    json.RawMessage(`0`),
		"zones":         json.RawMessage(`[{"name":"Asia/Shanghai","long":"121.47"},{"name":"UTC","long":"0"}]`),
		"privilege_id":  json.RawMessage(`4`),
	}
}

func (f *fakeWebBMC) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)

	switch {
	case r.URL.Path == "/api/session" && r.Method == http.MethodPost:
		f.logins++
		if f.loginBody != "" {
			w.Header().Set("Content-Type", f.loginContentType)
			w.WriteHeader(f.loginStatus)
			_, _ = io.WriteString(w, f.loginBody)
			return
		}
		// 注意：body 已在上面被 ReadAll 吃掉，这里必须用读到的字节自己解表单，
		// 再调 r.ParseForm() 只会拿到空值（上一版就栽在这里，表现为"所有登录都 401"）。
		form, err := url.ParseQuery(string(body))
		if err != nil ||
			form.Get("username") != fakeWebUser || form.Get("password") != fakeWebPass {
			respondJSON(w, http.StatusUnauthorized, map[string]any{"cc": 7, "error": "Invalid Authentication"})
			return
		}
		f.sessionValid = true
		http.SetCookie(w, &http.Cookie{Name: "QSESSIONID", Value: "sess-abc", Path: "/", HttpOnly: true})
		respondJSON(w, http.StatusOK, map[string]any{
			"ok": 0, "CSRFToken": fakeCSRF, "privilege": "Administrator", "privilege_id": 4,
			"user_id": 2, "channel": 0, "server_addr": "192.168.48.134",
			"passwordStatus": 1, "TFAEnabled": 0, "TFAStatus": 0,
		})

	case r.URL.Path == "/api/session" && r.Method == http.MethodDelete:
		f.deletes++
		if f.deleteStatus != http.StatusNoContent {
			respondJSON(w, f.deleteStatus, map[string]any{"cc": 7, "error": "Invalid Authentication"})
			return
		}
		f.sessionValid = false
		w.WriteHeader(http.StatusNoContent)

	case r.URL.Path == "/api/settings/date-time" && r.Method == http.MethodGet:
		f.gets++
		f.lastGetHdr = r.Header.Clone()
		if !f.sessionValid {
			respondJSON(w, http.StatusUnauthorized, map[string]any{"cc": 7, "error": "Invalid Authentication"})
			return
		}
		respondJSON(w, http.StatusOK, f.doc)

	case r.URL.Path == "/api/settings/date-time" && r.Method == http.MethodPut:
		f.puts++
		f.lastPutHdr = r.Header.Clone()
		f.lastPutRaw = string(body)
		var got map[string]json.RawMessage
		if err := json.Unmarshal(body, &got); err != nil {
			respondJSON(w, http.StatusBadRequest, map[string]any{"error": "Invalid API Call", "code": 1010})
			return
		}
		f.lastPut = got
		if f.putStatus != http.StatusOK {
			respondJSON(w, f.putStatus, map[string]any{"error": "Invalid API Call", "code": 1010})
			return
		}
		if f.putSwallow {
			return // 静默吞写：200 但不改状态
		}
		for k, v := range got {
			f.doc[k] = v // 全量替换语义
		}
		w.WriteHeader(http.StatusOK)

	default:
		respondJSON(w, http.StatusNotFound, map[string]any{"error": "Invalid API Call", "code": 1010})
	}
}

func (f *fakeWebBMC) client() *WebClient {
	f.t.Helper()
	return NewWebClient(f.s.URL, fakeWebUser, fakeWebPass, WithInsecureTLS(true))
}

func (f *fakeWebBMC) mustLogin(t *testing.T) *WebClient {
	t.Helper()
	w := f.client()
	if err := w.Login(context.Background()); err != nil {
		t.Fatalf("Login(): %v", err)
	}
	return w
}

func TestWebLoginStoresSessionAndSendsCSRF(t *testing.T) {
	f := newFakeWebBMC(t)
	w := f.mustLogin(t)
	if !w.LoggedIn() {
		t.Fatal("Login 成功后 LoggedIn 应为 true")
	}
	if f.logins != 1 {
		t.Fatalf("应只发一次登录，实发 %d", f.logins)
	}
	if _, err := w.GetDateTime(context.Background()); err != nil {
		t.Fatalf("GetDateTime(): %v", err)
	}
	if got := f.lastGetHdr.Get(webCSRFHeader); got != fakeCSRF {
		t.Fatalf("后续请求应带 %s=%s，实得 %q", webCSRFHeader, fakeCSRF, got)
	}
	// garc 镜像 cookie 必须真的发出去（web 层可能按 cookie/头配对校验）
	if ck := f.lastGetHdr.Get("Cookie"); !strings.Contains(ck, webCSRFCookie+"="+fakeCSRF) {
		t.Fatalf("请求 Cookie 里缺少 %s=%s，实得 %q", webCSRFCookie, fakeCSRF, ck)
	}
	if ck := f.lastGetHdr.Get("Cookie"); !strings.Contains(ck, "QSESSIONID=sess-abc") {
		t.Fatalf("请求 Cookie 里缺少会话 QSESSIONID，实得 %q", ck)
	}
}

func TestWebLoginOKSemantics(t *testing.T) {
	// SPA 原文：ok===0 才是成功；ok!=0 && passwordStatus==1 是强制改密；TFA 未过要单独可辨
	cases := []struct {
		name    string
		body    string
		wantErr error
		wantMsg string
	}{
		{"ok=0 且 passwordStatus=1 仍算成功", `{"ok":0,"CSRFToken":"t1","passwordStatus":1}`, nil, ""},
		{"ok!=0 且要求改密", `{"ok":1,"passwordStatus":1}`, ErrPasswordChangeRequired, ""},
		{"会话数已满", `{"ok":1,"code":1018}`, nil, "ok=1"},
		{"TFA 未通过", `{"ok":0,"TFAEnabled":1,"TFAStatus":0,"CSRFToken":"t1"}`, ErrTFARequired, ""},
		{"成功但没有 CSRFToken", `{"ok":0}`, nil, "CSRFToken"},
		{"缺 ok 字段", `{"privilege":"Administrator"}`, nil, "ok"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeWebBMC(t)
			f.loginBody = tc.body
			err := f.client().Login(context.Background())
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("应得 %v，实得 %v", tc.wantErr, err)
				}
				return
			}
			if tc.wantMsg == "" {
				if err != nil {
					t.Fatalf("应成功，实得 %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Fatalf("错误里应含 %q，实得 %v", tc.wantMsg, err)
			}
		})
	}
}

// TestWebLoginLighttpd501 是真实风险的回归位：本机实测 curl 打 POST /api/session 一律 501，
// 错误里必须带得上原始状态码与 HTML 里的原因，不能退化成一句"登录失败"。
func TestWebLoginLighttpd501(t *testing.T) {
	f := newFakeWebBMC(t)
	f.loginStatus = http.StatusNotImplemented
	f.loginContentType = "text/html"
	f.loginBody = `<?xml version="1.0"?><html><head><title>501 - Not Implemented</title></head><body>x</body></html>`
	err := f.client().Login(context.Background())
	if err == nil {
		t.Fatal("501 必须报错")
	}
	for _, want := range []string{"HTTP 501", "Not Implemented"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误信息应含 %q，实得 %v", want, err)
		}
	}
	var we *WebError
	if !errors.As(err, &we) || !we.IsUnsupported() {
		t.Fatalf("501 应可被判为 IsUnsupported，实得 %v", err)
	}
}

func TestWebErrorPayloadShapes(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantMsg    string
		wantCC     int
		wantCCOk   bool
		wantCode   int
		wantCodeOk bool
	}{
		{"IPMI 鉴权层", 401, `{ "cc": 7, "error": "Invalid Authentication" }`, "Invalid Authentication", 7, true, 0, false},
		{"API 路由层", 404, `{ "error": "Invalid API Call", "code": 1010 }`, "Invalid API Call", 0, false, 1010, true},
		{"lighttpd HTML", 501, `<html><title>501 - Not Implemented</title></html>`, "501 - Not Implemented", 0, false, 0, false},
		{"未知载荷不丢信息", 500, `boom && 中文尾巴`, "boom && 中文尾巴", 0, false, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			we := newWebError(tc.status, "/api/settings/date-time", []byte(tc.body))
			if !strings.Contains(we.Message, tc.wantMsg) {
				t.Fatalf("Message 应含 %q，实得 %q", tc.wantMsg, we.Message)
			}
			if we.CC != tc.wantCC || we.CCHas != tc.wantCCOk {
				t.Fatalf("cc 应为 %d/%v，实得 %d/%v", tc.wantCC, tc.wantCCOk, we.CC, we.CCHas)
			}
			if we.Code != tc.wantCode || we.CodeS != tc.wantCodeOk {
				t.Fatalf("code 应为 %d/%v，实得 %d/%v", tc.wantCode, tc.wantCodeOk, we.Code, we.CodeS)
			}
			if !strings.Contains(we.Error(), "HTTP") {
				t.Fatalf("Error() 应含状态码，实得 %q", we.Error())
			}
		})
	}
}

func TestWebErrorPredicates(t *testing.T) {
	if !newWebError(401, "/x", []byte(`{"cc":7}`)).IsUnauthorized() {
		t.Fatal("401 应判为鉴权失败")
	}
	if !newWebError(200, "/x", []byte(`{"cc":7,"error":"x"}`)).IsUnauthorized() {
		t.Fatal("cc=7 即使 HTTP 200 也应判为鉴权失败")
	}
	for _, st := range []int{404, 405, 501} {
		if !newWebError(st, "/x", nil).IsUnsupported() {
			t.Fatalf("%d 应判为本机不支持", st)
		}
	}
	if newWebError(500, "/x", nil).IsUnsupported() {
		t.Fatal("500 是服务端故障，不能判成\"本机没有这条路\"")
	}
}

// TestSupportsDateTimeProbe 固化免认证三态判据（401=存在、404/501=不存在）
func TestSupportsDateTimeProbe(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    bool
		wantErr bool
	}{
		{"401 表示路由存在且需会话", 401, `{"cc":7,"error":"Invalid Authentication"}`, true, false},
		{"404 code1010 表示没有该路由", 404, `{"error":"Invalid API Call","code":1010}`, false, false},
		{"501 表示 web 层不放该方法", 501, `<title>501 - Not Implemented</title>`, false, false},
		{"200 匿名可读也算存在", 200, `{"timezone":"UTC"}`, true, false},
		{"500 是故障，不能当成不支持", 500, `boom`, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeWebBMC(t)
			f.s.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			got, err := f.client().SupportsDateTime(context.Background())
			if got != tc.want {
				t.Fatalf("SupportsDateTime()=%v，期望 %v（err=%v）", got, tc.want, err)
			}
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v，期望有错=%v", err, tc.wantErr)
			}
			if f.logins != 0 {
				t.Fatal("探针必须免认证，不许顺手登录")
			}
		})
	}
}

func TestGetDateTimeKeepsRawBytes(t *testing.T) {
	f := newFakeWebBMC(t)
	w := f.mustLogin(t)
	d, err := w.GetDateTime(context.Background())
	if err != nil {
		t.Fatalf("GetDateTime(): %v", err)
	}
	if d.Timezone() != "GMT+08" {
		t.Fatalf("timezone 应为 GMT+08，实得 %q", d.Timezone())
	}
	if v := d.UTCTotalMinutes(); v == nil || *v != -480 {
		t.Fatalf("utc_minutes 应为 -480，实得 %v", v)
	}
	// 数字必须逐字节保真：经 map[string]any 会走 float64，尾巴和小数都不复存在
	raw, ok := d.Get("timestamp")
	if !ok || string(raw) != "1755238694.0000000001" {
		t.Fatalf("timestamp 原始字节被改写: %s", raw)
	}
	if !d.Has("zones") {
		t.Fatal("只读的 zones 也应保留（全量回写要带上）")
	}
	// 排序按 sort.Strings 的字节序：timestamp < timezone（第 5 位 's' < 'z'）——本行期望值一度被
	// 按直觉写反，是这条断言把它拽回来的，别再凭印象改。
	want := []string{"id", "ntp_auto_date", "primary_ntp", "privilege_id", "ptp_auto_date", "ptp_preset",
		"secondary_ntp", "timestamp", "timezone", "utc_minutes", "zones"}
	got := d.Keys()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Keys() 不符:\n got=%v\nwant=%v", got, want)
	}
	if s := d.Summary(); !strings.Contains(s, "timezone=\"GMT+08\"") || !strings.Contains(s, "utc_minutes=-480") {
		t.Fatalf("Summary() 不符: %s", s)
	}
}

func TestGetDateTimeRequiresLogin(t *testing.T) {
	f := newFakeWebBMC(t)
	if _, err := f.client().GetDateTime(context.Background()); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("未登录应得 ErrNotLoggedIn，实得 %v", err)
	}
	if f.gets != 0 {
		t.Fatalf("未登录时不该发请求，实发 GET %d 次", f.gets)
	}
}

func TestSetTimeZoneFullEcho(t *testing.T) {
	f := newFakeWebBMC(t)
	w := f.mustLogin(t)
	res, err := w.SetTimeZone(context.Background(), "Asia/Shanghai", nil)
	if err != nil {
		t.Fatalf("SetTimeZone(): %v", err)
	}
	if !res.Applied || res.Before != "GMT+08" || res.After != "Asia/Shanghai" {
		t.Fatalf("结果不符: %+v", res)
	}
	if strings.Join(res.ChangedKeys, ",") != "timezone" {
		t.Fatalf("只应改 timezone，实得 %v", res.ChangedKeys)
	}
	if f.puts != 1 {
		t.Fatalf("应恰好一次 PUT，实发 %d", f.puts)
	}
	// 全量回送：原有每个键都要出现在 PUT body 里
	cur := defaultWebDoc()
	for k := range cur {
		if _, ok := f.lastPut[k]; !ok {
			t.Fatalf("PUT body 丢了字段 %s（PUT 是全量替换，丢字段等于清空该配置）", k)
		}
	}
	// 未参与本次写入的值必须逐字节原样
	for _, k := range []string{"timestamp", "zones", "primary_ntp", "secondary_ntp", "ntp_auto_date", "id", "privilege_id"} {
		if string(f.lastPut[k]) != string(cur[k]) {
			t.Fatalf("%s 被改写了:\n got=%s\nwant=%s", k, f.lastPut[k], cur[k])
		}
	}
	if string(f.lastPut["timezone"]) != `"Asia/Shanghai"` {
		t.Fatalf("timezone 写入值不符: %s", f.lastPut["timezone"])
	}
	// utc_minutes 传 nil ⇒ 原样带回，绝不自己猜符号
	if string(f.lastPut["utc_minutes"]) != string(cur["utc_minutes"]) {
		t.Fatalf("utc_minutes 不该被动过: got=%s want=%s", f.lastPut["utc_minutes"], cur["utc_minutes"])
	}
	if res.UTCMinutesBefore == nil || *res.UTCMinutesBefore != -480 || res.UTCMinutesAfter == nil || *res.UTCMinutesAfter != -480 {
		t.Fatalf("utc_minutes 前后值不符: %+v", res)
	}
	if got := f.lastPutHdr.Get(webCSRFHeader); got != fakeCSRF {
		t.Fatalf("PUT 应带 CSRF 头，实得 %q", got)
	}
}

func TestSetTimeZoneWithUTCMinutes(t *testing.T) {
	f := newFakeWebBMC(t)
	w := f.mustLogin(t)
	min := 480
	res, err := w.SetTimeZone(context.Background(), "Asia/Shanghai", &min)
	if err != nil {
		t.Fatalf("SetTimeZone(): %v", err)
	}
	if strings.Join(res.ChangedKeys, ",") != "timezone,utc_minutes" {
		t.Fatalf("应改两个字段，实得 %v", res.ChangedKeys)
	}
	if string(f.lastPut["utc_minutes"]) != "480" {
		t.Fatalf("utc_minutes 应写成 480，实得 %s", f.lastPut["utc_minutes"])
	}
	if res.UTCMinutesAfter == nil || *res.UTCMinutesAfter != 480 {
		t.Fatalf("回读的 utc_minutes 应为 480，实得 %v", res.UTCMinutesAfter)
	}
}

// TestSetTimeZoneSwallowed 覆盖最阴的失败模式：固件回 200 但根本没理这个字段。
// 若只信 err == nil，界面就会报"成功"而时区其实没改。
func TestSetTimeZoneSwallowed(t *testing.T) {
	f := newFakeWebBMC(t)
	f.putSwallow = true
	w := f.mustLogin(t)
	res, err := w.SetTimeZone(context.Background(), "Asia/Shanghai", nil)
	if err != nil {
		t.Fatalf("静默吞写不是错误，应返回 nil，实得 %v", err)
	}
	if res.Applied {
		t.Fatal("固件没采纳，Applied 必须为 false")
	}
	if res.After != "GMT+08" || res.Before != "GMT+08" {
		t.Fatalf("前后值都应保持 GMT+08，实得 %+v", res)
	}
	if res.Note == "" {
		t.Fatal("未生效必须给出诊断信息")
	}
}

func TestSetTimeZonePutRejected(t *testing.T) {
	f := newFakeWebBMC(t)
	f.putStatus = http.StatusBadRequest
	w := f.mustLogin(t)
	res, err := w.SetTimeZone(context.Background(), "Asia/Shanghai", nil)
	if err == nil {
		t.Fatal("PUT 被拒应报错")
	}
	var we *WebError
	if !errors.As(err, &we) || we.Code != 1010 {
		t.Fatalf("应带上私有 API 的 code=1010，实得 %v", err)
	}
	if res == nil || res.Applied {
		t.Fatalf("被拒时 Applied 必须为 false，实得 %+v", res)
	}
}

// TestSetTimeZoneAbortsWhenSchemaMismatch：schema 与预期不符时一个写请求都不许发。
func TestSetTimeZoneAbortsWhenSchemaMismatch(t *testing.T) {
	f := newFakeWebBMC(t)
	f.doc = map[string]json.RawMessage{"timestamp": json.RawMessage(`1755238694`), "zones": json.RawMessage(`[]`)}
	w := f.mustLogin(t)
	res, err := w.SetTimeZone(context.Background(), "Asia/Shanghai", nil)
	if !errors.Is(err, ErrNoTimezoneField) {
		t.Fatalf("应得 ErrNoTimezoneField，实得 %v", err)
	}
	if res == nil || res.Applied {
		t.Fatalf("Applied 必须为 false，实得 %+v", res)
	}
	if f.puts != 0 {
		t.Fatalf("schema 不符时不许发 PUT，实发 %d 次", f.puts)
	}
}

func TestSetTimeZoneRejectsBadNames(t *testing.T) {
	f := newFakeWebBMC(t)
	w := f.mustLogin(t)
	bad := []string{"", "Asia Shanghai", `Shanghai";`, "GMT+08\n", strings.Repeat("A", 65), "北京"}
	for _, name := range bad {
		if _, err := w.SetTimeZone(context.Background(), name, nil); err == nil {
			t.Fatalf("时区名 %q 应被拒绝", name)
		}
	}
	// 合法形态：IANA 名、Etc/GMT 变体、下拉里的手动偏移
	for _, name := range []string{"Asia/Shanghai", "Etc/GMT-8", "UTC", "GMT+08", "Asia/Hong_Kong"} {
		if _, err := w.SetTimeZone(context.Background(), name, nil); err != nil {
			t.Fatalf("合法时区名 %q 被误拒: %v", name, err)
		}
	}
	if _, err := f.client().SetTimeZone(context.Background(), "UTC", nil); !errors.Is(err, ErrNotLoggedIn) {
		t.Fatalf("未登录的客户端应得 ErrNotLoggedIn，实得 %v", err)
	}
}

func TestLogout(t *testing.T) {
	t.Run("正常登出", func(t *testing.T) {
		f := newFakeWebBMC(t)
		w := f.mustLogin(t)
		if err := w.Logout(context.Background()); err != nil {
			t.Fatalf("Logout(): %v", err)
		}
		if w.LoggedIn() {
			t.Fatal("登出后 LoggedIn 应为 false")
		}
		if f.deletes != 1 {
			t.Fatalf("应发一次 DELETE，实发 %d", f.deletes)
		}
		if err := w.Logout(context.Background()); err != nil {
			t.Fatalf("重复登出应幂等，实得 %v", err)
		}
		if f.deletes != 1 {
			t.Fatalf("已登出不该再发 DELETE，实发 %d", f.deletes)
		}
	})
	t.Run("会话已失效算成功", func(t *testing.T) {
		f := newFakeWebBMC(t)
		f.deleteStatus = http.StatusUnauthorized
		w := f.mustLogin(t)
		if err := w.Logout(context.Background()); err != nil {
			t.Fatalf("会话本就不在了应算幂等成功，实得 %v", err)
		}
		if w.LoggedIn() {
			t.Fatal("登出后应清除本地会话状态")
		}
	})
}

func TestSetTimeZoneOverHTTPSErrorIsNotSilent(t *testing.T) {
	// GET 阶段就 500：必须原样把故障传出去，不能返回空结果让上层以为"没数据"
	f := newFakeWebBMC(t)
	w := f.mustLogin(t)
	f.s.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		respondJSON(rw, http.StatusInternalServerError, map[string]any{"error": "Internal Error"})
	})
	if _, err := w.SetTimeZone(context.Background(), "Asia/Shanghai", nil); err == nil {
		t.Fatal("GET 故障必须报错")
	} else if !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("错误里应含状态码，实得 %v", err)
	}
}

func TestUTCTotalMinutesFor(t *testing.T) {
	for tz, want := range map[string]int{"Asia/Shanghai": 480, "UTC": 0, "Asia/Kolkata": 330, "Asia/Tokyo": 540} {
		got, err := UTCTotalMinutesFor(tz)
		if err != nil {
			t.Fatalf("%s: %v", tz, err)
		}
		if got != want {
			t.Fatalf("%s 应为 %d（东为正），实得 %d", tz, want, got)
		}
	}
	if _, err := UTCTotalMinutesFor("Mars/Olympus"); err == nil {
		t.Fatal("不存在的时区应报错")
	}
	if _, err := UTCTotalMinutesFor("bad name"); err == nil {
		t.Fatal("非法时区名应报错")
	}
}

func TestWebClientHostNormalization(t *testing.T) {
	if got := NewWebClient("192.168.48.134", "u", "p").Host(); got != "https://192.168.48.134" {
		t.Fatalf("裸 IP 应补 https:// ，实得 %s", got)
	}
	if got := NewWebClient("https://192.168.48.134/", "u", "p").Host(); got != "https://192.168.48.134" {
		t.Fatalf("结尾斜杠应去掉，实得 %s", got)
	}
}

// TestDateTimeRawRoundTrip 全量回写的地基：读到的对象再序列化必须与读到的逐字节一致，
// 且 Raw 暴露的就是内部那份数据。这里断言的是"不改写"，不是"能序列化"。
func TestDateTimeRawRoundTrip(t *testing.T) {
	f := newFakeWebBMC(t)
	w := f.mustLogin(t)
	d, err := w.GetDateTime(context.Background())
	if err != nil {
		t.Fatalf("GetDateTime(): %v", err)
	}
	if len(d.Raw()) != len(d.Keys()) {
		t.Fatalf("Raw() 与 Keys() 数量不符: %d vs %d", len(d.Raw()), len(d.Keys()))
	}
	out, err := d.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON(): %v", err)
	}
	for _, frag := range []string{`"timestamp":1755238694.0000000001`, `"timezone":"GMT+08"`, `"utc_minutes":-480`} {
		if !strings.Contains(string(out), frag) {
			t.Fatalf("回写基座丢了 %s，实得 %s", frag, out)
		}
	}
	// 空对象也不该炸成 panic
	if b, err := (&DateTimeSettings{}).MarshalJSON(); err != nil || string(b) != "null" {
		t.Fatalf("空 DateTimeSettings 应编成 null，实得 %s / %v", b, err)
	}
}

// TestWebErrorRedfishStyleErrorObject：有些固件在私有路径上也会回 Redfish 那种 error 对象，
// 该分支不能只存在于代码里。
func TestWebErrorRedfishStyleErrorObject(t *testing.T) {
	we := newWebError(400, "/api/settings/date-time",
		[]byte(`{"error":{"code":"Base.1.12.PropertyUnknown","message":"该属性不存在"}}`))
	if we.Message != "该属性不存在" {
		t.Fatalf("应取到 error.message，实得 %q", we.Message)
	}
	if !strings.Contains(we.Error(), "HTTP 400") {
		t.Fatalf("Error() 应含状态码，实得 %q", we.Error())
	}
}

// TestDateTimeMalformedFields：字段存在但类型不对时，取值一律降级为空值而不是 panic；
// 写流程照样把坏值覆盖掉（存在即覆盖），而**只有回读真的读到目标值**才判生效——
// 固件若静默吞写，回读仍是坏值，必须判未生效，绝不把"取不出"当成成功。
func TestDateTimeMalformedFields(t *testing.T) {
	f := newFakeWebBMC(t)
	f.doc = map[string]json.RawMessage{
		"timezone":    json.RawMessage(`123`), // 类型不符
		"utc_minutes": json.RawMessage(`"x"`), // 类型不符
		"timestamp":   json.RawMessage(`1`),
	}
	w := f.mustLogin(t)
	d, err := w.GetDateTime(context.Background())
	if err != nil {
		t.Fatalf("GetDateTime(): %v", err)
	}
	if d.Timezone() != "" {
		t.Fatalf("非字符串 timezone 应取空，实得 %q", d.Timezone())
	}
	if d.UTCTotalMinutes() != nil {
		t.Fatalf("非数字 utc_minutes 应取 nil，实得 %v", *d.UTCTotalMinutes())
	}
	if s := d.Summary(); !strings.Contains(s, "utc_minutes=<absent>") {
		t.Fatalf("Summary 应标注 utc_minutes 缺失，实得 %q", s)
	}
	// 类型不符但字段存在 ⇒ 仍然执行"读-改-全量回写"，把坏值覆盖掉。
	// 固件采纳时判成功是对的：回读拿到的是我们写进去的合法字符串，不存在"取不出"。
	res, err := w.SetTimeZone(context.Background(), "Asia/Shanghai", nil)
	if err != nil {
		t.Fatalf("SetTimeZone(): %v", err)
	}
	if !res.Applied || res.After != "Asia/Shanghai" {
		t.Fatalf("固件采纳后应判生效，实得 %+v", res)
	}
	if res.Before != "" {
		t.Fatalf("Before 取不出合法值时应为空串，实得 %q", res.Before)
	}

	// 真正危险的组合：进来的值就是坏的，且固件静默吞写 ⇒ 回读仍是那个取不出值的坏值，
	// 此时 Applied 必须为 false，绝不能把"取不出"当成生效。
	f2 := newFakeWebBMC(t)
	f2.doc = map[string]json.RawMessage{
		"timezone":    json.RawMessage(`123`),
		"utc_minutes": json.RawMessage(`"x"`),
		"timestamp":   json.RawMessage(`1`),
	}
	f2.putSwallow = true
	w2 := f2.mustLogin(t)
	res2, err := w2.SetTimeZone(context.Background(), "Asia/Shanghai", nil)
	if err != nil {
		t.Fatalf("静默吞写不是错误，实得 %v", err)
	}
	if res2.Applied {
		t.Fatalf("回读值取不出时不得判成功，实得 %+v", res2)
	}
	if res2.After != "" {
		t.Fatalf("After 应为空串，实得 %q", res2.After)
	}
	if !strings.Contains(res2.Note, "静默吞写") {
		t.Fatalf("应给出静默吞写的说明，实得 %q", res2.Note)
	}
}

func TestWebSnippetIsRuneSafe(t *testing.T) {
	// 按字节截断会把中文劈成半个字符，产出的字符串进日志/界面就是乱码
	long := strings.Repeat("时钟", 200)
	got := webSnippet([]byte(long), 10)
	if !strings.HasSuffix(got, "…") || !strings.Contains(got, "时钟") || strings.ContainsRune(got, utf8.RuneError) {
		t.Fatalf("截断结果不合法: %q", got)
	}
	if len([]rune(got)) != 11 { // 10 个字符 + 省略号
		t.Fatalf("截断长度不符: %d", len([]rune(got)))
	}
	if short := webSnippet([]byte("短"), 10); short != "短" {
		t.Fatalf("短文本不该被加工: %q", short)
	}
}

func TestWebDoRejectsRelativePath(t *testing.T) {
	f := newFakeWebBMC(t)
	w := f.client()
	if _, _, err := w.do(context.Background(), http.MethodGet, "api/settings/date-time", nil, ""); err == nil {
		t.Fatal("相对路径必须被拒（否则会拼成 host+api/... 打到错误的主机）")
	}
}

// TestSetTimeZoneReadbackFails：写入请求被接受但回读失败，必须报"无法判定"，
// 不能让上层把"没报错"当成"已生效"。
func TestSetTimeZoneReadbackFails(t *testing.T) {
	f := newFakeWebBMC(t)
	w := f.mustLogin(t)
	base := f.s.Config.Handler
	var puts int
	f.s.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			puts++
			base.ServeHTTP(rw, r)
			return
		}
		if puts > 0 { // PUT 之后的回读全部故障
			respondJSON(rw, http.StatusInternalServerError, map[string]any{"error": "Internal Error"})
			return
		}
		base.ServeHTTP(rw, r)
	})
	res, err := w.SetTimeZone(context.Background(), "Asia/Shanghai", nil)
	if err == nil {
		t.Fatal("回读失败必须报错")
	}
	if res == nil || !strings.Contains(res.Note, "回读失败") {
		t.Fatalf("应给出\"无法判定\"的诊断，实得 %+v", res)
	}
	if res.Applied {
		t.Fatal("回读失败时不得判成功")
	}
}
