package redfish

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeBootBMC 模拟 AST2600 的引导顺序场景（真机行为复刻）：
//   - 主资源带 @Redfish.Settings 注解（Boot 属性实际写在 SD 资源上）
//   - SD 资源写 Boot 属性时强制要求 BootSourceOverrideTarget/Enabled/Mode 三件套
//   - BootOrder 里出现固件不认识的元素名时 PATCH 照样 2xx，但顺序不落盘
//     （真机上就是拿 Id 短号 "000E" 去写 → 204 且顺序原样不动）
type fakeBootBMC struct {
	mu sync.Mutex

	bootOrder           []string // 固件「生效顺序」（主资源 = NVRAM 当前值）
	sdBootOrder         []string // 未来状态（SD 资源 = 下次启动生效的顺序）；nil = SD 尚未写过 Boot
	diskOption          string   // 磁盘引导项成员名（AST2600 形态 "Boot000E"，也是 BootOrder 里的元素名）
	diskOptID           string   // 磁盘引导项 Id（AST2600 形态短号 "000E"）
	networkOnly         bool     // true = 集合里只有网络引导项
	diskOnly            bool     // true = 集合里只有磁盘引导项
	requireArgs         bool     // true = SD 路径缺三件套即 400
	sdPatchFail         bool     // true = SD 写 BootOrder 400（验证回退主资源）
	mainPatchFail       bool     // true = 主资源写 BootOrder 400
	bootOrderReject     bool     // true = 所有含 BootOrder 的 PATCH 都 400（验证回退一次性覆盖）
	mainOverrideFail    bool     // true = 主资源拒写一次性引导覆盖（验证回退 Settings 路径）
	silentDropBootOrder bool     // true = 固件 2xx 却不落盘（顺序原样不动，回读必须报错）
	legacy              bool     // true = SecureBoot 404（Legacy 固件）
	netOptMAC           string   // 给 PXE 引导项的设备路径带上 MAC（真机 UefiDevicePath 含 MAC(...)）
	noSettings          bool     // true = 主资源不暴露 @Redfish.Settings 注解（无 SD 资源）

	sdBoot        map[string]any
	patches       []string
	patchPayloads []map[string]any
}

func (f *fakeBootBMC) handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/redfish/v1/Systems", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{
			"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Self"}},
		})
	})
	mux.HandleFunc("/redfish/v1/Systems/Self", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("ETag", `"stable"`)
			body := map[string]any{
				"Id":   "Self",
				"Boot": map[string]any{"BootOrder": f.bootOrder},
			}
			if !f.noSettings {
				body["@Redfish.Settings"] = map[string]any{
					"SettingsObject": map[string]any{"@odata.id": "/redfish/v1/Systems/Self/SD"},
				}
			}
			respondJSON(w, 200, body)
		case http.MethodPatch:
			f.applyPatch(w, r, false)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/redfish/v1/Systems/Self/SD", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("ETag", `"stable"`)
			boot := map[string]any{}
			for k, v := range f.sdBoot {
				if k == "BootOrder" {
					continue // BootOrder 只认「真正落盘」的那份（固件 2xx 却不落盘时序不变）
				}
				boot[k] = v
			}
			if f.sdBootOrder != nil {
				boot["BootOrder"] = f.sdBootOrder
			}
			respondJSON(w, 200, map[string]any{"Id": "SD", "Boot": boot})
		case http.MethodPatch:
			f.applyPatch(w, r, true)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/redfish/v1/Systems/Self/SecureBoot", func(w http.ResponseWriter, r *http.Request) {
		if f.legacy {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		respondJSON(w, 200, map[string]any{"Id": "SecureBoot", "SecureBootMode": "SetupMode"})
	})
	mux.HandleFunc("/redfish/v1/Systems/Self/BootOptions", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		members := []map[string]string{}
		if !f.diskOnly {
			members = append(members,
				map[string]string{"@odata.id": "/redfish/v1/Systems/Self/BootOptions/Boot0002"},
				map[string]string{"@odata.id": "/redfish/v1/Systems/Self/BootOptions/Boot0003"},
			)
		}
		if !f.networkOnly {
			members = append(members, map[string]string{
				"@odata.id": "/redfish/v1/Systems/Self/BootOptions/" + f.diskOption,
			})
		}
		respondJSON(w, 200, map[string]any{"Members": members})
	})
	mux.HandleFunc("/redfish/v1/Systems/Self/BootOptions/Boot0002", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{
			"Id": "0002", "Name": "Boot0002", "DisplayName": "UEFI: HTTP IP4 P0",
			"UefiDevicePath": "IPv4(0,0,0,0,0)",
		})
	})
	mux.HandleFunc("/redfish/v1/Systems/Self/BootOptions/Boot0003", func(w http.ResponseWriter, r *http.Request) {
		dp := "IPv4(0,0,0,0,0)"
		if f.netOptMAC != "" {
			dp = "PciRoot(0x0)/Pci(0xD,0x0)/Pci(0x0,0x0)/MAC(" + f.netOptMAC + ",0x1)/" + dp
		}
		respondJSON(w, 200, map[string]any{
			"Id": "0003", "Name": "Boot0003", "DisplayName": "UEFI: PXE IP4 P0",
			"UefiDevicePath": dp,
		})
	})
	mux.HandleFunc("/redfish/v1/Systems/Self/BootOptions/"+f.diskOption, func(w http.ResponseWriter, r *http.Request) {
		// AST2600 真实形态：Id 是短号（"000E"），Name 才是 BootOrder 里用的元素名（"Boot000E"），
		// DisplayName 才是人看的引导项名（"ubuntu"）。
		respondJSON(w, 200, map[string]any{
			"Id": f.diskOptID, "Name": f.diskOption, "DisplayName": "ubuntu",
			"UefiDevicePath": "HD(15,GPT,8D06DF08-F231-47C6-91D9-A7A030574DEE,0x2800,0x35000)/\\EFI\\ubuntu\\shimx64.efi",
		})
	})
	return mux
}

func (f *fakeBootBMC) applyPatch(w http.ResponseWriter, r *http.Request, isSD bool) {
	var body map[string]any
	b, _ := io.ReadAll(r.Body)
	_ = json.Unmarshal(b, &body)
	boot, _ := body["Boot"].(map[string]any)
	if isSD && f.requireArgs {
		for _, k := range []string{"BootSourceOverrideTarget", "BootSourceOverrideEnabled", "BootSourceOverrideMode"} {
			if _, ok := boot[k]; !ok {
				w.WriteHeader(http.StatusBadRequest)
				respondJSON(w, http.StatusBadRequest, map[string]any{"Message": "The property " + k + " is a required property"})
				return
			}
		}
	}
	if _, ok := boot["BootSourceOverrideTarget"]; ok && !isSD && f.mainOverrideFail {
		w.WriteHeader(http.StatusBadRequest)
		respondJSON(w, http.StatusBadRequest, map[string]any{"Message": "System rejects BootSourceOverrideTarget"})
		return
	}
	if order, ok := boot["BootOrder"].([]any); ok {
		if f.silentDropBootOrder {
			// 真机行为：固件返回 2xx，顺序却不落盘（只回读能发现）
			f.sdBoot = boot
			f.patches = append(f.patches, r.URL.Path)
			f.patchPayloads = append(f.patchPayloads, body)
			w.WriteHeader(http.StatusOK)
			return
		}
		if f.bootOrderReject {
			w.WriteHeader(http.StatusBadRequest)
			respondJSON(w, http.StatusBadRequest, map[string]any{"Message": "The property BootOrder is not writable"})
			return
		}
		if isSD && f.sdPatchFail {
			w.WriteHeader(http.StatusBadRequest)
			respondJSON(w, http.StatusBadRequest, map[string]any{"Message": "SD rejects BootOrder"})
			return
		}
		if !isSD && f.mainPatchFail {
			w.WriteHeader(http.StatusBadRequest)
			respondJSON(w, http.StatusBadRequest, map[string]any{"Message": "System rejects BootOrder"})
			return
		}
		out := make([]string, 0, len(order))
		for _, v := range order {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		if f.allEntriesKnown(out) {
			// 写 SD = 改未来状态；写主资源 = 改固件生效顺序，两者互不影响
			if isSD {
				f.sdBootOrder = out
			} else {
				f.bootOrder = out
			}
		}
	}
	f.sdBoot = boot
	f.patches = append(f.patches, r.URL.Path)
	f.patchPayloads = append(f.patchPayloads, body)
	w.WriteHeader(http.StatusOK)
}

// allEntriesKnown 判断写入的引导顺序里每个元素都是固件已知的引导项名
func (f *fakeBootBMC) allEntriesKnown(order []string) bool {
	known := map[string]bool{"Boot0002": true, "Boot0003": true, f.diskOption: true}
	for _, e := range f.bootOrder {
		known[e] = true
	}
	for _, e := range f.sdBootOrder {
		known[e] = true
	}
	for _, e := range order {
		if !known[e] {
			return false
		}
	}
	return true
}

func newFakeBootServer(t *testing.T, f *fakeBootBMC) *Client {
	t.Helper()
	ts := httptest.NewTLSServer(f.handler())
	t.Cleanup(ts.Close)
	return NewClient(ts.URL, "admin", "pass", WithInsecureTLS(true))
}

func defaultFakeBoot() *fakeBootBMC {
	return &fakeBootBMC{
		bootOrder:   []string{"Boot0002", "Boot0003", "Boot000E"},
		diskOption:  "Boot000E",
		diskOptID:   "000E",
		requireArgs: true,
	}
}

// ---------- 切回本地盘引导 ----------

func TestSetSystemBootToDiskPromotesDiskEntry(t *testing.T) {
	f := defaultFakeBoot()
	c := newFakeBootServer(t, f)
	ctx := context.Background()

	st, err := c.SetSystemBootToDisk(ctx, "")
	if err != nil {
		t.Fatalf("SetSystemBootToDisk(): %v", err)
	}
	if !st.Written || !st.NeedsRestart {
		t.Fatalf("状态不符: %+v", err)
	}
	if st.Entry != "Boot000E" {
		t.Fatalf("置首元素=%q 期望 Boot000E", st.Entry)
	}
	// 写的是 SD（未来状态），主资源不动
	if len(f.sdBootOrder) == 0 || f.sdBootOrder[0] != "Boot000E" {
		t.Fatalf("SD 未来状态未把磁盘项置首: %v", f.sdBootOrder)
	}
	if f.bootOrder[0] != "Boot0002" {
		t.Fatalf("主资源生效顺序被误改: %v", f.bootOrder)
	}
	// SD 写入必须带三件套
	var found bool
	for i, p := range f.patches {
		if p == "/redfish/v1/Systems/Self/SD" {
			found = true
			boot := f.patchPayloads[i]["Boot"].(map[string]any)
			for _, k := range []string{"BootSourceOverrideTarget", "BootSourceOverrideEnabled", "BootSourceOverrideMode"} {
				if _, ok := boot[k]; !ok {
					t.Fatalf("SD 写入缺 %s: %+v", k, boot)
				}
			}
		}
	}
	if !found {
		t.Fatalf("未写 SD 资源: %v", f.patches)
	}
}

func TestSetSystemBootToDiskSkipsWriteWhenPendingAlreadyDiskFirst(t *testing.T) {
	f := defaultFakeBoot()
	f.sdBootOrder = []string{"Boot000E", "Boot0002", "Boot0003"}
	c := newFakeBootServer(t, f)

	st, err := c.SetSystemBootToDisk(context.Background(), "")
	if err != nil {
		t.Fatalf("SetSystemBootToDisk(): %v", err)
	}
	if st.Written || st.NeedsRestart {
		t.Fatalf("已置首不应改写: %+v", st)
	}
	if len(f.patches) != 0 {
		t.Fatalf("不应发起写请求: %v", f.patches)
	}
	if st.ConfiguredFirst != "Boot000E" {
		t.Fatalf("ConfiguredFirst=%q 期望 Boot000E", st.ConfiguredFirst)
	}
}

func TestSetSystemBootToDiskFailsWhenNoDiskOption(t *testing.T) {
	f := defaultFakeBoot()
	f.networkOnly = true
	c := newFakeBootServer(t, f)

	if _, err := c.SetSystemBootToDisk(context.Background(), ""); err == nil {
		t.Fatal("没有磁盘引导项时应报错")
	}
}

func TestSetSystemBootToDiskFallsBackToMainResource(t *testing.T) {
	f := defaultFakeBoot()
	f.sdPatchFail = true
	c := newFakeBootServer(t, f)

	st, err := c.SetSystemBootToDisk(context.Background(), "")
	if err != nil {
		t.Fatalf("SetSystemBootToDisk(): %v", err)
	}
	if !st.Written {
		t.Fatalf("应改写成功: %+v", st)
	}
	if f.bootOrder[0] != "Boot000E" {
		t.Fatalf("主资源未把磁盘项置首: %v", f.bootOrder)
	}
}

func TestSetSystemBootToDiskFailsWhenFirmwareRejects(t *testing.T) {
	f := defaultFakeBoot()
	f.sdPatchFail = true
	f.mainPatchFail = true
	c := newFakeBootServer(t, f)

	if _, err := c.SetSystemBootToDisk(context.Background(), ""); err == nil {
		t.Fatal("固件拒写时应报错")
	}
}

// ---------- 切到网络引导（PXE） ----------

func TestSetSystemBootToNetworkUsesPreferredMAC(t *testing.T) {
	f := defaultFakeBoot()
	f.netOptMAC = "9C6B004A3817"
	c := newFakeBootServer(t, f)

	st, err := c.SetSystemBootToNetwork(context.Background(), "", "9c:6b:00:4a:38:17")
	if err != nil {
		t.Fatalf("SetSystemBootToNetwork(): %v", err)
	}
	if st.Entry != "Boot0003" {
		t.Fatalf("置首元素=%q 期望 Boot0003（按 MAC 精确匹配 PXE 项）", st.Entry)
	}
	if len(f.sdBootOrder) == 0 || f.sdBootOrder[0] != "Boot0003" {
		t.Fatalf("SD 未来状态未把网络项置首: %v", f.sdBootOrder)
	}
}

func TestSetSystemBootToNetworkFallsBackToFirstNetworkOption(t *testing.T) {
	f := defaultFakeBoot()
	c := newFakeBootServer(t, f)

	st, err := c.SetSystemBootToNetwork(context.Background(), "", "")
	if err != nil {
		t.Fatalf("SetSystemBootToNetwork(): %v", err)
	}
	// Boot0002 是 HTTP 项、Boot0003 是 PXE 项：关键字优先 PXE
	if st.Entry != "Boot0003" {
		t.Fatalf("置首元素=%q 期望 Boot0003（PXE 优先）", st.Entry)
	}
}

func TestSetSystemBootToNetworkFailsWhenNoNetworkOption(t *testing.T) {
	f := defaultFakeBoot()
	f.diskOnly = true
	c := newFakeBootServer(t, f)

	if _, err := c.SetSystemBootToNetwork(context.Background(), "", ""); err == nil {
		t.Fatal("没有网络引导项时应报错")
	}
}

func TestSetSystemBootToNetworkAlreadyFirstIsIdempotent(t *testing.T) {
	f := defaultFakeBoot()
	f.bootOrder = []string{"Boot0003", "Boot0002", "Boot000E"}
	f.sdBootOrder = []string{"Boot0003", "Boot0002", "Boot000E"}
	c := newFakeBootServer(t, f)

	st, err := c.SetSystemBootToNetwork(context.Background(), "", "")
	if err != nil {
		t.Fatalf("SetSystemBootToNetwork(): %v", err)
	}
	if st.Written {
		t.Fatalf("已在首位不应改写: %+v", st)
	}
	if len(f.patches) != 0 {
		t.Fatalf("不应发起写请求: %v", f.patches)
	}
}

// ---------- 一次性引导覆盖 ----------

func TestSetSystemBootOverrideOnceWritesMainResource(t *testing.T) {
	f := defaultFakeBoot()
	c := newFakeBootServer(t, f)

	if err := c.SetSystemBootOverrideOnce(context.Background(), "", BootPxe, ""); err != nil {
		t.Fatalf("SetSystemBootOverrideOnce(): %v", err)
	}
	if len(f.patches) != 1 || f.patches[0] != "/redfish/v1/Systems/Self" {
		t.Fatalf("应只写主资源一次: %v", f.patches)
	}
	boot := f.patchPayloads[0]["Boot"].(map[string]any)
	if boot["BootSourceOverrideTarget"] != BootPxe || boot["BootSourceOverrideEnabled"] != BootOverrideOnce {
		t.Fatalf("一次性覆盖 payload 不符: %+v", boot)
	}
}

func TestSetSystemBootOverrideOnceFallsBackToSettingsPath(t *testing.T) {
	f := defaultFakeBoot()
	f.requireArgs = true
	f.mainOverrideFail = true // 主资源拒写一次性覆盖 → 走 SD
	c := newFakeBootServer(t, f)

	if err := c.SetSystemBootOverrideOnce(context.Background(), "", BootPxe, ""); err != nil {
		t.Fatalf("SetSystemBootOverrideOnce(): %v", err)
	}
	var sdSeen bool
	for i, p := range f.patches {
		if p != "/redfish/v1/Systems/Self/SD" {
			continue
		}
		sdSeen = true
		boot := f.patchPayloads[i]["Boot"].(map[string]any)
		if boot["BootSourceOverrideMode"] != BootModeUEFI {
			t.Fatalf("SD 写入缺引导模式: %+v", boot)
		}
	}
	if !sdSeen {
		t.Fatalf("未写 SD 资源: %v", f.patches)
	}
}

// ---------- 元素名解析与工具函数 ----------

func TestResolveBootEntry(t *testing.T) {
	order := []string{"Boot0002", "Boot0003", "Boot000E"}
	cases := []struct {
		name string
		opt  *BootOption
		want string
	}{
		{"按 Name 精确匹配", &BootOption{ID: "000E", Name: "Boot000E"}, "Boot000E"},
		{"按 Id 精确匹配", &BootOption{ID: "0003", Name: "Boot9999"}, "Boot0003"},
		{"按 Boot+Id 后缀匹配", &BootOption{ID: "000E", Name: "Boot9999"}, "Boot000E"},
		{"回退 Name", &BootOption{ID: "000E", Name: "Boot000E"}, "Boot000E"},
	}
	for _, tc := range cases {
		if got := ResolveBootEntry(tc.opt, order); got != tc.want {
			t.Errorf("%s: ResolveBootEntry=%q want=%q", tc.name, got, tc.want)
		}
	}
}

func TestPromoteBootEntryKeepsRelativeOrder(t *testing.T) {
	got := PromoteBootEntry([]string{"A", "B", "C", "D"}, "C")
	want := []string{"C", "A", "B", "D"}
	if len(got) != len(want) {
		t.Fatalf("长度=%d want=%d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("PromoteBootEntry=%v want=%v", got, want)
		}
	}
}

func TestNormalizeMAC(t *testing.T) {
	cases := map[string]string{
		"9c:6b:00:4a:38:17": "9C6B004A3817",
		"9C-6B-00-4A-38-17": "9C6B004A3817",
		"9c6b.004a.3817":    "9C6B004A3817",
		"9C6B004A3817":      "9C6B004A3817",
		"":                  "",
		"bad":               "",
	}
	for in, want := range cases {
		if got := NormalizeMAC(in); got != want {
			t.Errorf("NormalizeMAC(%q)=%q want=%q", in, got, want)
		}
	}
}

func TestDevicePathHasMAC(t *testing.T) {
	dp := "PciRoot(0x0)/Pci(0xD,0x0)/Pci(0x0,0x0)/MAC(9C6B004A3817,0x1)/IPv4(0,0,0,0,0)"
	if !DevicePathHasMAC(dp, "9C6B004A3817") {
		t.Error("应匹配到 MAC")
	}
	if DevicePathHasMAC(dp, "9C6B004A3816") {
		t.Error("不应匹配到其它 MAC")
	}
	if DevicePathHasMAC("IPv4(0,0,0,0,0)", "9C6B004A3817") {
		t.Error("无 MAC 的设备路径不应匹配")
	}
}

func TestIsNetworkBootOption(t *testing.T) {
	if !IsNetworkBootOption(&BootOption{Name: "Boot0003", DisplayName: "UEFI: PXE IP4 P0"}) {
		t.Error("PXE 项应判为网络引导项")
	}
	if IsNetworkBootOption(&BootOption{Name: "Boot000E", DisplayName: "ubuntu", UefiDevicePath: "HD(15,GPT,...)/\\EFI\\ubuntu\\shimx64.efi"}) {
		t.Error("磁盘项不应判为网络引导项")
	}
}

func TestPickDiskBootOptionPrefersHDPath(t *testing.T) {
	opts := []*BootOption{
		{Name: "Boot0003", DisplayName: "UEFI: PXE IP4 P0"},
		{Name: "Boot000E", DisplayName: "ubuntu", UefiDevicePath: "HD(15,GPT,...)/\\EFI\\ubuntu\\shimx64.efi"},
	}
	got, err := PickDiskBootOption(opts)
	if err != nil {
		t.Fatalf("PickDiskBootOption(): %v", err)
	}
	if got.Name != "Boot000E" {
		t.Fatalf("选中=%q 期望 Boot000E", got.Name)
	}
}

func TestSystemBootOrderViewDistinguishesPendingOrder(t *testing.T) {
	f := defaultFakeBoot()
	f.sdBootOrder = []string{"Boot000E", "Boot0002", "Boot0003"}
	c := newFakeBootServer(t, f)

	v, err := c.SystemBootOrderView(context.Background(), "")
	if err != nil {
		t.Fatalf("SystemBootOrderView(): %v", err)
	}
	if v.SettingsPath != "/redfish/v1/Systems/Self/SD" {
		t.Fatalf("SettingsPath=%q", v.SettingsPath)
	}
	if len(v.Effective) == 0 || v.Effective[0] != "Boot0002" {
		t.Fatalf("生效顺序=%v", v.Effective)
	}
	if len(v.Future) == 0 || v.Future[0] != "Boot000E" {
		t.Fatalf("未来状态=%v", v.Future)
	}
	if len(v.Configured) == 0 || v.Configured[0] != "Boot000E" {
		t.Fatalf("下次生效顺序=%v", v.Configured)
	}
}

func TestSystemBootOverrideModeLegacyFirmware(t *testing.T) {
	f := defaultFakeBoot()
	f.legacy = true
	c := newFakeBootServer(t, f)

	mode, err := c.SystemBootOverrideMode(context.Background(), "")
	if err != nil {
		t.Fatalf("SystemBootOverrideMode(): %v", err)
	}
	if mode != BootModeLegacy {
		t.Fatalf("mode=%q 期望 %q", mode, BootModeLegacy)
	}
}

func TestPatchWithIfMatchFallsBackToWildcard(t *testing.T) {
	var seen []string
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("ETag", `"stable"`)
			w.WriteHeader(http.StatusOK)
			return
		}
		seen = append(seen, r.Header.Get("If-Match"))
		if r.Header.Get("If-Match") == `"stable"` {
			w.WriteHeader(http.StatusPreconditionFailed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "admin", "pass", WithInsecureTLS(true))
	err := c.PatchWithIfMatch(context.Background(), "/redfish/v1/Systems/Self", map[string]any{"Boot": map[string]any{"BootOrder": []string{"Boot000E"}}})
	if err != nil {
		t.Fatalf("PatchWithIfMatch(): %v", err)
	}
	if len(seen) != 2 || seen[0] != `"stable"` || seen[1] != "*" {
		t.Fatalf("If-Match 序列=%v 期望 [\"stable\" *]", seen)
	}
}

func TestBootOrderWriteRejectsUnknownEntry(t *testing.T) {
	// 固件返回 2xx 却不落盘（顺序原样不动）⇒ 回读校验必须报错，不能把「2xx」当成功
	f := defaultFakeBoot()
	f.silentDropBootOrder = true
	c := newFakeBootServer(t, f)

	_, err := c.SetSystemBootToDisk(context.Background(), "")
	if err == nil {
		t.Fatal("落盘校验失败时应报错")
	}
	if !strings.Contains(err.Error(), "Boot000E") {
		t.Fatalf("错误信息应含目标元素名: %v", err)
	}
}
