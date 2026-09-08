package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func respondJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// mockBMC 用 httptest 模拟 AST2600 BMC 的核心端点行为
type mockBMC struct {
	*httptest.Server
	// 请求记录（断言请求体用）
	lastBody   map[string]json.RawMessage
	lastMethod string
	lastPath   string
	// ClearLog 调用计数
	clearLogCalls int32
	// Task 状态迁移：第 N 次查询返回 Running，之后 Completed
	taskPolls int32
}

func newMockBMC(t *testing.T) *mockBMC {
	t.Helper()
	m := &mockBMC{}
	mux := http.NewServeMux()

	// ServiceRoot（精确匹配，其余路径 404）
	mux.HandleFunc("/redfish/v1/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/redfish/v1/" {
			respondJSON(w, 404, map[string]any{"error": map[string]any{"message": "资源不存在"}})
			return
		}
		respondJSON(w, 200, map[string]any{
			"@odata.id": "/redfish/v1/", "RedfishVersion": "1.11.0", "Vendor": "AMI",
			"Systems": map[string]string{"@odata.id": "/redfish/v1/Systems"},
		})
	})

	// 会话登录
	mux.HandleFunc("/redfish/v1/SessionService/Sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondJSON(w, 200, map[string]any{"Members": []any{}, "Members@odata.count": 0})
			return
		}
		var req map[string]string
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["UserName"] != "admin" || req["Password"] != "pass" {
			respondJSON(w, 401, map[string]any{"error": map[string]any{
				"code": "Base.1.12.AuthenticationFailed", "message": "用户名或密码错误"}})
			return
		}
		w.Header().Set("X-Auth-Token", "tok-123")
		w.Header().Set("Location", "/redfish/v1/SessionService/Sessions/abc")
		respondJSON(w, 201, map[string]any{
			"@odata.id": "/redfish/v1/SessionService/Sessions/abc", "Id": "abc", "UserName": "admin"})
	})
	mux.HandleFunc("/redfish/v1/SessionService/Sessions/abc", func(w http.ResponseWriter, r *http.Request) {
		// 模拟 session-only 部署：DELETE 会话必须携带 X-Auth-Token（R2-1 回归防线）
		if r.Method == http.MethodDelete && r.Header.Get("X-Auth-Token") == "" {
			respondJSON(w, 401, map[string]any{"error": map[string]any{"message": "缺少 X-Auth-Token"}})
			return
		}
		respondJSON(w, 204, nil)
	})

	// Systems 集合 + 实例
	mux.HandleFunc("/redfish/v1/Systems", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Self"}}, "Members@odata.count": 1})
	})
	mux.HandleFunc("/redfish/v1/Systems/Self", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			record(m, r)
			respondJSON(w, 204, nil)
			return
		}
		respondJSON(w, 200, map[string]any{
			"@odata.id": "/redfish/v1/Systems/Self", "Id": "Self", "PowerState": "On",
			"Manufacturer": "ASRockRack", "Model": "GENOAD8UD-2T/X550", "SerialNumber": "80G400010019",
			"Boot": map[string]any{"BootSourceOverrideTarget": nil, "BootOrder": []string{"Boot0000"}},
		})
	})

	// Reset action → 202 + Task
	mux.HandleFunc("/redfish/v1/Systems/Self/Actions/ComputerSystem.Reset", func(w http.ResponseWriter, r *http.Request) {
		record(m, r)
		respondJSON(w, 202, map[string]any{
			"@odata.id": "/redfish/v1/TaskService/Tasks/1", "TaskState": "New", "Name": "Computer Reset"})
	})

	// Task 轮询：第一次 Running，第二次 Completed
	mux.HandleFunc("/redfish/v1/TaskService/Tasks/1", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&m.taskPolls, 1)
		if n == 1 {
			respondJSON(w, 200, map[string]any{"@odata.id": "/redfish/v1/TaskService/Tasks/1", "TaskState": "Running"})
			return
		}
		respondJSON(w, 200, map[string]any{
			"@odata.id": "/redfish/v1/TaskService/Tasks/1", "TaskState": "Completed", "TaskStatus": "OK"})
	})

	// Manager / EthernetInterfaces
	mux.HandleFunc("/redfish/v1/Managers", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/Self"}}, "Members@odata.count": 1})
	})
	mux.HandleFunc("/redfish/v1/Managers/Self", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"@odata.id": "/redfish/v1/Managers/Self", "Id": "Self",
			"FirmwareVersion": "5.06.00", "ManagerType": "BMC"})
	})
	mux.HandleFunc("/redfish/v1/Managers/Self/EthernetInterfaces", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Members": []map[string]string{
			{"@odata.id": "/redfish/v1/Managers/Self/EthernetInterfaces/bond0"}}, "Members@odata.count": 1})
	})
	mux.HandleFunc("/redfish/v1/Managers/Self/EthernetInterfaces/bond0", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			record(m, r)
			respondJSON(w, 204, nil)
			return
		}
		respondJSON(w, 200, map[string]any{
			"Id": "bond0", "MACAddress": "00:00:00:00:00:01",
			"IPv4Addresses": []map[string]any{{"Address": "192.168.1.100", "SubnetMask": "255.255.255.0", "AddressOrigin": "DHCP"}},
		})
	})

	// Chassis + Power/Thermal
	mux.HandleFunc("/redfish/v1/Chassis", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Chassis/Self"}}, "Members@odata.count": 1})
	})
	mux.HandleFunc("/redfish/v1/Chassis/Self", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"@odata.id": "/redfish/v1/Chassis/Self", "Id": "Self", "PowerState": "On"})
	})
	mux.HandleFunc("/redfish/v1/Chassis/Self/Power", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{
			"PowerControl": []map[string]any{{"PowerConsumedWatts": 350.5, "PowerCapacityWatts": 800}},
			"Voltages":     []map[string]any{{"MemberId": "0", "ReadingVolts": 12.1, "SensorNumber": 1}},
		})
	})
	mux.HandleFunc("/redfish/v1/Chassis/Self/Thermal", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{
			"Temperatures": []map[string]any{{"MemberId": "0", "Name": "CPU1 Temp", "ReadingCelsius": 45.5, "PhysicalContext": "CPU"}},
			"Fans":         []map[string]any{{"MemberId": "0", "Name": "Fan1", "Reading": 5000, "ReadingUnits": "RPM"}},
		})
	})

	// PCIeDevices 分页：第一页 2 个成员 + nextLink，第二页 1 个
	mux.HandleFunc("/redfish/v1/Chassis/Self/PCIeDevices", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("$skip") == "2" {
			respondJSON(w, 200, map[string]any{"Members": []map[string]string{
				{"@odata.id": "/redfish/v1/Chassis/Self/PCIeDevices/00_02_00"}}, "Members@odata.count": 1})
			return
		}
		respondJSON(w, 200, map[string]any{
			"Members": []map[string]string{
				{"@odata.id": "/redfish/v1/Chassis/Self/PCIeDevices/00_00_00"},
				{"@odata.id": "/redfish/v1/Chassis/Self/PCIeDevices/00_01_00"}},
			"Members@odata.count":    3,
			"Members@odata.nextLink": "/redfish/v1/Chassis/Self/PCIeDevices?$skip=2"})
	})

	// VirtualMedia
	mux.HandleFunc("/redfish/v1/Managers/Self/VirtualMedia", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Managers/Self/VirtualMedia/CD1"}}, "Members@odata.count": 1})
	})
	mux.HandleFunc("/redfish/v1/Managers/Self/VirtualMedia/CD1/Actions/VirtualMedia.InsertMedia", func(w http.ResponseWriter, r *http.Request) {
		record(m, r)
		respondJSON(w, 202, map[string]any{"@odata.id": "/redfish/v1/TaskService/Tasks/1", "TaskState": "New"})
	})
	mux.HandleFunc("/redfish/v1/Managers/Self/VirtualMedia/CD1/Actions/VirtualMedia.EjectMedia", func(w http.ResponseWriter, r *http.Request) {
		record(m, r)
		respondJSON(w, 204, nil)
	})

	// UpdateService 根（精确）
	mux.HandleFunc("/redfish/v1/UpdateService", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{
			"@odata.id": "/redfish/v1/UpdateService", "Id": "UpdateService",
			"MultipartHttpPushUri": "/redfish/v1/UpdateService/upload",
			"Oem":                  map[string]any{"AMIUpdateService": map[string]any{"FlashPercentage": nil}},
		})
	})
	// UpdateService/upload：multipart → 202 + Messages 携带任务 URI
	mux.HandleFunc("/redfish/v1/UpdateService/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/redfish/v1/UpdateService/upload" {
			respondJSON(w, 404, map[string]any{"error": map[string]any{"message": "资源不存在"}})
			return
		}
		body, _ := io.ReadAll(r.Body)
		m.lastBody = map[string]json.RawMessage{"__multipart__": body}
		m.lastMethod, m.lastPath = r.Method, r.URL.Path
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "multipart/form-data") {
			respondJSON(w, 400, map[string]any{"error": map[string]any{"message": "须为 multipart"}})
			return
		}
		respondJSON(w, 202, map[string]any{"@odata.type": "#UpdateService.v1_6_0.UpdateService", "Messages": []map[string]any{
			{"MessageId": "Task.1.0.New", "MessageArgs": []string{"/redfish/v1/TaskService/Tasks/2"}},
		}})
	})
	mux.HandleFunc("/redfish/v1/TaskService/Tasks/2", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"TaskState": "Completed", "TaskStatus": "OK"})
	})

	// Volume 创建（断言 Oem.Ami.Device）
	mux.HandleFunc("/redfish/v1/Systems/Self/Storage/StorageUnit_0/Volumes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			record(m, r)
			respondJSON(w, 204, nil)
			return
		}
		respondJSON(w, 200, map[string]any{"Members": []map[string]string{
			{"@odata.id": "/redfish/v1/Systems/Self/Storage/StorageUnit_0/Volumes/VOL0"}}, "Members@odata.count": 1})
	})

	// EventService 订阅
	mux.HandleFunc("/redfish/v1/EventService/Subscriptions", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			record(m, r)
			respondJSON(w, 201, map[string]any{"@odata.id": "/redfish/v1/EventService/Subscriptions/1", "Id": "1",
				"Destination": "http://192.168.1.1:5000/event"})
			return
		}
		respondJSON(w, 200, map[string]any{"Members": []any{}, "Members@odata.count": 0})
	})

	// 账户
	mux.HandleFunc("/redfish/v1/AccountService/Accounts", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			record(m, r)
			respondJSON(w, 201, map[string]any{"@odata.id": "/redfish/v1/AccountService/Accounts/2", "Id": "2", "UserName": "newuser"})
			return
		}
		respondJSON(w, 200, map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/AccountService/Accounts/1"}}, "Members@odata.count": 1})
	})
	mux.HandleFunc("/redfish/v1/AccountService/Accounts/1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			respondJSON(w, 204, nil)
			return
		}
		respondJSON(w, 200, map[string]any{"@odata.id": "/redfish/v1/AccountService/Accounts/1", "Id": "1", "UserName": "admin", "RoleId": "Administrator"})
	})

	// BIOS 属性（FBO）
	mux.HandleFunc("/redfish/v1/Systems/Self/Bios", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Attributes": map[string]any{
			"FBO001": "FBO001UEFI", "FBO201": "FBO201HardDisk", "FBO202": "FBO202NVME",
		}})
	})
	mux.HandleFunc("/redfish/v1/Systems/Self/Bios/SD", func(w http.ResponseWriter, r *http.Request) {
		record(m, r)
		respondJSON(w, 204, nil)
	})

	// SEL 日志
	mux.HandleFunc("/redfish/v1/Chassis/Self/LogServices", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Chassis/Self/LogServices/Logs"}}, "Members@odata.count": 1})
	})
	mux.HandleFunc("/redfish/v1/Chassis/Self/LogServices/Logs", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"@odata.id": "/redfish/v1/Chassis/Self/LogServices/Logs", "Id": "Logs", "LogEntryType": "SEL"})
	})
	mux.HandleFunc("/redfish/v1/Chassis/Self/LogServices/Logs/Entries", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Members": []map[string]any{
			{"Id": "1", "EntryType": "SEL", "Severity": "Critical", "Message": "#0x09 | 01h | Event Data 1 = 01h", "SensorType": "Temperature", "SensorNumber": 42},
		}, "Members@odata.count": 1})
	})
	mux.HandleFunc("/redfish/v1/Chassis/Self/LogServices/Logs/Actions/LogService.ClearLog", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&m.clearLogCalls, 1)
		respondJSON(w, 202, map[string]any{"@odata.id": "/redfish/v1/TaskService/Tasks/1", "TaskState": "New"})
	})

	mux.HandleFunc("/redfish/v1/Systems/Self/Memory", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Members": []map[string]string{{"@odata.id": "/redfish/v1/Systems/Self/Memory/0"}}, "Members@odata.count": 1})
	})
	mux.HandleFunc("/redfish/v1/Systems/Self/Memory/0", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{
			"@odata.id": "/redfish/v1/Systems/Self/Memory/0", "Id": "0", "CapacityMiB": 32768,
			"MemoryDeviceType": "DDR4", "Metrics": map[string]string{"@odata.id": "/redfish/v1/Systems/Self/Memory/0/Metrics"},
		})
	})
	mux.HandleFunc("/redfish/v1/Systems/Self/Memory/0/Metrics", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{
			"@odata.id": "/redfish/v1/Systems/Self/Memory/0/Metrics", "Id": "Metrics", "Type": "DRAM",
			"TemperatureCelsius": 45.5, "CorrectableECCError": 2, "SparesRemaining": 98,
		})
	})

	m.Server = httptest.NewServer(mux)
	t.Cleanup(m.Server.Close)
	return m
}

// record 记录 JSON 请求体供断言
func record(m *mockBMC, r *http.Request) {
	m.lastMethod, m.lastPath = r.Method, r.URL.Path
	body, _ := io.ReadAll(r.Body)
	m.lastBody = map[string]json.RawMessage{}
	_ = json.Unmarshal(body, &m.lastBody)
}

func newTestClient(m *mockBMC) *Client {
	return NewClient(m.Server.URL, "admin", "pass")
}

func TestSystemDiscoverAndRead(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	sys, err := c.System(ctx, "")
	if err != nil {
		t.Fatalf("System(): %v", err)
	}
	if sys.ID != "Self" || sys.SerialNumber != "80G400010019" || sys.PowerState != PowerOn {
		t.Fatalf("系统字段不符: %+v", sys)
	}
}

func TestSetSystemIndicatorLEDPatch(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	// IndicatorLED 是可写属性，必须走 PATCH（POST 到资源根 URI 在真实 BMC 上必返 405/404）
	if _, err := c.SetSystemIndicatorLED(ctx, "", "Blinking"); err != nil {
		t.Fatalf("SetSystemIndicatorLED(): %v", err)
	}
	if m.lastMethod != http.MethodPatch {
		t.Fatalf("方法应为 PATCH，实际 %s", m.lastMethod)
	}
	if string(m.lastBody["IndicatorLED"]) != `"Blinking"` {
		t.Fatalf("IndicatorLED 请求体不符: %s", m.lastBody["IndicatorLED"])
	}
}

func TestStatusPredicatesUnwrap(t *testing.T) {
	// 谓词须沿 %w 包装链查找 HTTPError（库内部 resolve 路径普遍包装）
	wrapped := fmt.Errorf("发现 system 失败: %w", &HTTPError{StatusCode: http.StatusNotFound, Path: "/redfish/v1/Systems"})
	if !IsNotFound(wrapped) {
		t.Fatal("IsNotFound 对包装错误应为 true")
	}
	if IsUnauthorized(wrapped) {
		t.Fatal("IsUnauthorized 对 404 包装错误应为 false")
	}
	if !IsNotFound(&HTTPError{StatusCode: http.StatusNotFound}) {
		t.Fatal("IsNotFound 对未包装错误应为 true")
	}
}

func TestSystemResetAndWaitTask(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	uri, err := c.SystemReset(ctx, "", ResetForceRestart)
	if err != nil {
		t.Fatalf("SystemReset(): %v", err)
	}
	if uri != "/redfish/v1/TaskService/Tasks/1" {
		t.Fatalf("任务 URI 不符: %s", uri)
	}
	// 请求体断言
	if string(m.lastBody["ResetType"]) != `"ForceRestart"` {
		t.Fatalf("ResetType 请求体不符: %s", m.lastBody["ResetType"])
	}
	task, err := c.WaitTask(ctx, uri, time.Millisecond)
	if err != nil {
		t.Fatalf("WaitTask(): %v", err)
	}
	if !task.OK() {
		t.Fatalf("任务未成功完成: %+v", task)
	}
}

func TestSetBootOnce(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	if err := c.SetSystemBootOnce(ctx, "", BootPxe, BootModeUEFI); err != nil {
		t.Fatalf("SetSystemBootOnce(): %v", err)
	}
	var boot map[string]any
	if err := json.Unmarshal(m.lastBody["Boot"], &boot); err != nil {
		t.Fatalf("Boot 解析失败: %v", err)
	}
	if boot["BootSourceOverrideTarget"] != "Pxe" || boot["BootSourceOverrideEnabled"] != "Once" {
		t.Fatalf("Boot 请求体不符: %v", boot)
	}
}

func TestLoginLogout(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	s, err := c.Login(ctx)
	if err != nil {
		t.Fatalf("Login(): %v", err)
	}
	if s.UserName != "admin" || c.sessionToken != "tok-123" {
		t.Fatalf("会话建立不符: %+v token=%s", s, c.sessionToken)
	}
	if err := c.Logout(ctx); err != nil {
		t.Fatalf("Logout(): %v", err)
	}
	if c.sessionToken != "" {
		t.Fatal("登出后 token 应清空")
	}
}

func TestLoginBadPassword(t *testing.T) {
	m := newMockBMC(t)
	c := NewClient(m.Server.URL, "admin", "wrong")
	if _, err := c.Login(context.Background()); err == nil {
		t.Fatal("密码错误应返回错误")
	} else if !IsUnauthorized(err) {
		t.Fatalf("应为 401: %v", err)
	}
}

func TestHTTPErrorPayload(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	err := c.Get(context.Background(), "/redfish/v1/NoSuchResource", nil)
	if err == nil {
		t.Fatal("404 应返回错误")
	}
	if !IsNotFound(err) {
		t.Fatalf("应为 404: %v", err)
	}
}

func TestSetBMCStaticIPv4(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	err := c.SetBMCStaticIPv4(ctx, "", "", "192.168.50.10", "255.255.255.0", "192.168.50.1")
	if err != nil {
		t.Fatalf("SetBMCStaticIPv4(): %v", err)
	}
	if m.lastPath != "/redfish/v1/Managers/Self/EthernetInterfaces/bond0" {
		t.Fatalf("请求路径不符: %s", m.lastPath)
	}
	var addrs []map[string]any
	if err := json.Unmarshal(m.lastBody["IPv4Addresses"], &addrs); err != nil {
		t.Fatalf("IPv4Addresses 解析失败: %v", err)
	}
	if addrs[0]["Address"] != "192.168.50.10" || addrs[0]["SubnetMask"] != "255.255.255.0" {
		t.Fatalf("IPv4 请求体不符: %v", addrs)
	}
}

func TestPowerThermal(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	p, err := c.Power(ctx, "")
	if err != nil {
		t.Fatalf("Power(): %v", err)
	}
	if p.ConsumedWatts() != 350.5 {
		t.Fatalf("功耗不符: %v", p.ConsumedWatts())
	}
	th, err := c.Thermal(ctx, "")
	if err != nil {
		t.Fatalf("Thermal(): %v", err)
	}
	if len(th.Temperatures) != 1 || *th.Temperatures[0].ReadingCelsius != 45.5 {
		t.Fatalf("温度不符: %+v", th.Temperatures)
	}
}

func TestCollectionPagination(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	coll, err := c.CollectionAll(ctx, "/redfish/v1/Chassis/Self/PCIeDevices")
	if err != nil {
		t.Fatalf("CollectionAll(): %v", err)
	}
	if len(coll.Members) != 3 {
		t.Fatalf("翻页后应 3 个成员，实际 %d", len(coll.Members))
	}
}

func TestVirtualMedia(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	uri, err := c.InsertMedia(ctx, "", "CD1", InsertMediaOptions{
		Image:                "//192.168.36.224/OS/ubuntu.iso",
		TransferProtocolType: TransferCIFS,
		UserName:             "user",
		Password:             "pass",
	})
	if err != nil {
		t.Fatalf("InsertMedia(): %v", err)
	}
	if uri != "/redfish/v1/TaskService/Tasks/1" {
		t.Fatalf("任务 URI 不符: %s", uri)
	}
	if string(m.lastBody["UserName"]) != `"user"` {
		t.Fatalf("UserName 请求体不符: %s", m.lastBody["UserName"])
	}
	if err := c.EjectMedia(ctx, "", "CD1"); err != nil {
		t.Fatalf("EjectMedia(): %v", err)
	}
}

func TestUploadImageMultipart(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	uri, err := c.uploadImageBytes(ctx, []byte("FAKE_IMAGE_BYTES"), []string{FirmwareBMC}, "BMC")
	if err != nil {
		t.Fatalf("uploadImageBytes(): %v", err)
	}
	if uri != "/redfish/v1/TaskService/Tasks/2" {
		t.Fatalf("应从 Messages 提取任务 URI，实际: %s", uri)
	}
	// multipart 内容断言
	body := string(m.lastBody["__multipart__"])
	if !strings.Contains(body, `name="UpdateFile"`) {
		t.Fatal("multipart 缺 UpdateFile")
	}
	if !strings.Contains(body, "FAKE_IMAGE_BYTES") {
		t.Fatal("multipart 缺镜像内容")
	}
	if !strings.Contains(body, `Targets`) {
		t.Fatal("multipart 缺 UpdateParameters.Targets")
	}
	if !strings.Contains(body, `"ImageType":"BMC"`) {
		t.Fatal("multipart 缺 OemParameters.ImageType")
	}
}

func TestCreateVolume(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	err := c.CreateVolume(ctx, "", "StorageUnit_0", CreateVolumeOptions{
		Name: "VOL_RAID1", RAIDType: RAID1, StripSizeBytes: Strip64K,
		ReadCachePolicyType: "ReadAhead", WriteCachePolicyType: "WriteThrough",
		ControllerURI:  "/redfish/v1/Systems/Self/Storage/StorageUnit_0",
		Initialization: "No_Initialization", DiskCachePolicy: "Unchanged", AccessPolicy: "Read-Write",
		DeviceURIs: []string{"/redfish/v1/Systems/Self/Storage/StorageUnit_0/Drives/NVMe0"},
	})
	if err != nil {
		t.Fatalf("CreateVolume(): %v", err)
	}
	var oem struct {
		Ami struct {
			ControllerID string   `json:"ControllerID"`
			Device       []string `json:"Device"`
		} `json:"Ami"`
	}
	if err := json.Unmarshal(m.lastBody["Oem"], &oem); err != nil {
		t.Fatalf("Oem 解析失败: %v", err)
	}
	if oem.Ami.ControllerID == "" || len(oem.Ami.Device) != 1 {
		t.Fatalf("Oem.Ami 请求体不符: %+v", oem.Ami)
	}
}

func TestSubscribe(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	s, err := c.Subscribe(ctx, SubscribeOptions{
		Destination: "http://192.168.1.1:5000/event",
		Context:     "ops-cmdb",
	})
	if err != nil {
		t.Fatalf("Subscribe(): %v", err)
	}
	if s.ID != "1" {
		t.Fatalf("订阅 ID 不符: %s", s.ID)
	}
	if string(m.lastBody["Destination"]) != `"http://192.168.1.1:5000/event"` {
		t.Fatalf("Destination 请求体不符: %s", m.lastBody["Destination"])
	}
}

func TestAccountCRUD(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	a, err := c.CreateAccount(ctx, "newuser", "pw@123", RoleOperator, true)
	if err != nil {
		t.Fatalf("CreateAccount(): %v", err)
	}
	if a.ID != "2" || string(m.lastBody["RoleId"]) != `"Operator"` {
		t.Fatalf("创建账户不符: %+v", a)
	}
	accounts, err := c.Accounts(ctx)
	if err != nil || len(accounts) != 1 {
		t.Fatalf("Accounts(): %v %d", err, len(accounts))
	}
	if err := c.ChangePassword(ctx, "1", "newpw@123"); err != nil {
		t.Fatalf("ChangePassword(): %v", err)
	}
	if err := c.DeleteAccount(ctx, "1"); err != nil {
		t.Fatalf("DeleteAccount(): %v", err)
	}
}

func TestFixedBootOrder(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	fbo, err := c.GetFixedBootOrder(ctx, "")
	if err != nil {
		t.Fatalf("GetFixedBootOrder(): %v", err)
	}
	if fbo.Mode != "FBO001UEFI" || len(fbo.UEFI) != 2 {
		t.Fatalf("FBO 解析不符: %+v", fbo)
	}
	if err := c.SetFixedBootOrder(ctx, "", BIOSAttributes{"FBO202": "FBO202CDDVD"}); err != nil {
		t.Fatalf("SetFixedBootOrder(): %v", err)
	}
	var attrs map[string]string
	if err := json.Unmarshal(m.lastBody["Attributes"], &attrs); err != nil || attrs["FBO202"] != "FBO202CDDVD" {
		t.Fatalf("Attributes 请求体不符: %v", attrs)
	}
}

func TestLogEntriesAndClear(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	entries, err := c.LogEntries(ctx, "", "", "")
	if err != nil {
		t.Fatalf("LogEntries(): %v", err)
	}
	if len(entries) != 1 || entries[0].SensorType != "Temperature" {
		t.Fatalf("日志条目不符: %+v", entries)
	}
	if _, err := c.ClearLog(ctx, "", "", ""); err != nil {
		t.Fatalf("ClearLog(): %v", err)
	}
	if atomic.LoadInt32(&m.clearLogCalls) != 1 {
		t.Fatal("ClearLog 应被调用一次")
	}
}

func TestManagerAndFirmware(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	mgr, err := c.Manager(ctx, "")
	if err != nil || mgr.FirmwareVersion != "5.06.00" {
		t.Fatalf("Manager(): %v %+v", err, mgr)
	}
	// 未实现固件清单端点 → 404 错误类型正确
	err = c.Get(ctx, "/redfish/v1/UpdateService/FirmwareInventory", nil)
	if err == nil || !IsNotFound(err) {
		t.Fatalf("FirmwareInventory 应 404: %v", err)
	}
}

func TestErrorFormat(t *testing.T) {
	he := newHTTPError(401, "/redfish/v1/x", []byte(`{"error":{"code":"Base.1.12.AuthenticationFailed","message":"Authentication failed","@Message.ExtendedInfo":[{"MessageId":"Base.1.12.NoValidSession","Message":"No valid session"}]}}`))
	if he.Message != "Authentication failed" || len(he.Extended) != 1 || he.Extended[0].MessageID != "Base.1.12.NoValidSession" {
		t.Fatalf("错误解析不符: %+v", he)
	}
	if !strings.Contains(he.Error(), "HTTP 401") {
		t.Fatalf("错误文案不符: %s", he.Error())
	}
}

func TestISCSIBootSettingsToMap(t *testing.T) {
	port := 3260
	s := iSCSIBootSettings{
		IPAddressType:      "IPv4",
		InitiatorIPAddress: "192.168.233.250",
		PrimaryTarget:      iSCSIBootTarget{TargetName: "iqn.2023:test01", TargetIPAddress: "192.168.233.2", TargetTCPPort: &port},
	}
	m := s.ToMap()
	if m["IPAddressType"] != "IPv4" || m["InitiatorIPAddress"] != "192.168.233.250" {
		t.Fatalf("iSCSI 顶层字段不符: %v", m)
	}
	if m["PrimaryTargetName"] != "iqn.2023:test01" || m["PrimaryTargetTCPPort"] != 3260 {
		t.Fatalf("Primary 前缀字段不符: %v", m)
	}
	if _, ok := m["SecondaryTargetName"]; ok {
		t.Fatal("未设置的 Secondary 字段不应出现")
	}
}

func TestConfigureiSCSIBootRequest(t *testing.T) {
	m := newMockBMC(t)
	// SD 端点：任意 /SD PATCH 返回 204（动态注册）
	m.Server.Config.Handler.(*http.ServeMux).HandleFunc("/redfish/v1/Chassis/Self/NetworkAdapters/NIC0/NetworkDeviceFunctions/Func0/SD", func(w http.ResponseWriter, r *http.Request) {
		record(m, r)
		respondJSON(w, 204, nil)
	})
	c := newTestClient(m)
	ctx := context.Background()

	err := c.ConfigureiSCSIBoot(ctx, "/redfish/v1/Chassis/Self/NetworkAdapters/NIC0/NetworkDeviceFunctions/Func0", iSCSIBootSettings{
		IPAddressType: "IPv4", InitiatorName: "iqn.2023:initiator",
	})
	if err != nil {
		t.Fatalf("ConfigureiSCSIBoot(): %v", err)
	}
	if m.lastPath != "/redfish/v1/Chassis/Self/NetworkAdapters/NIC0/NetworkDeviceFunctions/Func0/SD" {
		t.Fatalf("SD 路径不符: %s", m.lastPath)
	}
	var iscsi map[string]any
	if err := json.Unmarshal(m.lastBody["iSCSIBoot"], &iscsi); err != nil {
		t.Fatalf("iSCSIBoot 解析失败: %v", err)
	}
	if iscsi["InitiatorName"] != "iqn.2023:initiator" {
		t.Fatalf("iSCSIBoot 请求体不符: %v", iscsi)
	}
}

func TestPortsAndLLDP(t *testing.T) {
	m := newMockBMC(t)
	m.Server.Config.Handler.(*http.ServeMux).HandleFunc("/redfish/v1/Chassis/Self/NetworkAdapters/NIC0/Ports", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Members": []map[string]string{
			{"@odata.id": "/redfish/v1/Chassis/Self/NetworkAdapters/NIC0/Ports/1"}}, "Members@odata.count": 1})
	})
	m.Server.Config.Handler.(*http.ServeMux).HandleFunc("/redfish/v1/Chassis/Self/NetworkAdapters/NIC0/Ports/1", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{
			"Id": "1", "Name": "Port 1", "LinkStatus": "LinkUp",
			"Ethernet": map[string]any{
				"AssociatedMACAddresses": []string{"00:11:22:33:44:55"},
				"LLDPReceive":            map[string]string{"ChassisId": "SW-ACC1", "PortId": "gi1/0/24"},
			},
		})
	})
	c := newTestClient(m)
	ctx := context.Background()

	ports, err := c.NetworkAdapterPorts(ctx, "", "NIC0")
	if err != nil {
		t.Fatalf("NetworkAdapterPorts(): %v", err)
	}
	if ports[0].Ethernet.LLDPReceive.ChassisID != "SW-ACC1" {
		t.Fatalf("LLDP 上联信息不符: %+v", ports[0].Ethernet.LLDPReceive)
	}
}

func TestCompositionService(t *testing.T) {
	m := newMockBMC(t)
	mux := m.Server.Config.Handler.(*http.ServeMux)
	mux.HandleFunc("/redfish/v1/CompositionService", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			record(m, r)
			respondJSON(w, 204, nil)
			return
		}
		respondJSON(w, 200, map[string]any{"Id": "CompositionService", "ServiceEnabled": false})
	})
	mux.HandleFunc("/redfish/v1/CompositionService/ResourceBlocks", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Members": []map[string]string{
			{"@odata.id": "/redfish/v1/CompositionService/ResourceBlocks/ComputeBlock"}}, "Members@odata.count": 1})
	})
	mux.HandleFunc("/redfish/v1/CompositionService/ResourceBlocks/ComputeBlock", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Id": "ComputeBlock", "ResourceBlockType": []string{"Compute"},
			"CompositionStatus": map[string]any{"CompositionState": "Unused"}})
	})
	c := newTestClient(m)
	ctx := context.Background()

	if err := c.SetCompositionServiceEnabled(ctx, true); err != nil {
		t.Fatalf("SetCompositionServiceEnabled(): %v", err)
	}
	blocks, err := c.ResourceBlocks(ctx)
	if err != nil || len(blocks) != 1 || blocks[0].ID != "ComputeBlock" {
		t.Fatalf("ResourceBlocks(): %v", err)
	}
	if err := c.ReserveResourceBlock(ctx, "ComputeBlock", true); err != nil {
		t.Fatalf("ReserveResourceBlock(): %v", err)
	}
}

func TestUploadCABundle(t *testing.T) {
	m := newMockBMC(t)
	m.Server.Config.Handler.(*http.ServeMux).HandleFunc("/redfish/v1/UpdateService/Actions/Oem/UpdateService.UploadCABundle", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		m.lastBody = map[string]json.RawMessage{"__multipart__": body}
		respondJSON(w, 204, nil)
	})
	c := newTestClient(m)

	tmp := t.TempDir() + "/ca.pem"
	if err := os.WriteFile(tmp, []byte("-----BEGIN CERTIFICATE-----"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.UploadCABundle(context.Background(), tmp); err != nil {
		t.Fatalf("UploadCABundle(): %v", err)
	}
	if !strings.Contains(string(m.lastBody["__multipart__"]), `name="ca_bundle"`) {
		t.Fatal("multipart 缺 ca_bundle 字段")
	}
}

func TestInsecureTLSTLSSTransport(t *testing.T) {
	// 回归防线：WithInsecureTLS 曾解引用 nil TLSClientConfig 直接 panic
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"@odata.id": "/redfish/v1/", "RedfishVersion": "1.11.0"})
	}))
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, "admin", "pass", WithInsecureTLS(true))
	root, err := c.Root(context.Background())
	if err != nil || root.RedfishVersion != "1.11.0" {
		t.Fatalf("InsecureTLS 直连自签 HTTPS: %v %+v", err, root)
	}
}

func TestResourceETagAndPatchWithETag(t *testing.T) {
	var lastIfMatch string
	var sawIfMatch bool
	mux := http.NewServeMux()
	// 响应头 ETag 来源（强校验器；BMC 实际只返回强 ETag，不支持弱校验器）
	mux.HandleFunc("/hdr", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		respondJSON(w, 200, map[string]any{"Name": "x"})
	})
	// 仅 body @odata.etag 来源
	mux.HandleFunc("/body", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"@odata.etag": `"v2"`, "Name": "x"})
	})
	// 不提供 ETag
	mux.HandleFunc("/none", func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, 200, map[string]any{"Name": "x"})
	})
	// PATCH：缺 If-Match 返 428（模拟强制校验厂商）
	mux.HandleFunc("/np", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			respondJSON(w, 405, nil)
			return
		}
		sawIfMatch = true
		lastIfMatch = r.Header.Get("If-Match")
		if lastIfMatch == "" {
			respondJSON(w, 428, map[string]any{"error": map[string]any{"message": "If-Match required"}})
			return
		}
		respondJSON(w, 204, nil)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	c := NewClient(ts.URL, "admin", "pass")
	ctx := context.Background()

	etag, err := c.ResourceETag(ctx, "/hdr")
	if err != nil || etag != "v1" {
		t.Fatalf("响应头 ETag: %q %v", etag, err)
	}
	etag, err = c.ResourceETag(ctx, "/body")
	if err != nil || etag != "v2" {
		t.Fatalf("body @odata.etag 兜底: %q %v", etag, err)
	}
	etag, err = c.ResourceETag(ctx, "/none")
	if err != nil || etag != "" {
		t.Fatalf("无 ETag 应返回空串: %q %v", etag, err)
	}

	if err := c.PatchWithETag(ctx, "/np", map[string]string{"a": "b"}, "v3"); err != nil {
		t.Fatalf("PatchWithETag: %v", err)
	}
	if !sawIfMatch || lastIfMatch != `"v3"` {
		t.Fatalf("If-Match 未附带或格式错: %q", lastIfMatch)
	}
	// 裸 PATCH（etag 空）在强制校验厂商上必须失败——快速失败不吞 428
	if err := c.PatchWithETag(ctx, "/np", map[string]string{"a": "b"}, ""); !statusIs(err, http.StatusPreconditionRequired) {
		t.Fatalf("缺 If-Match 应返回 428，得到 err=%v", err)
	}
}

func TestMemoryAndMemoryMetrics(t *testing.T) {
	m := newMockBMC(t)
	c := newTestClient(m)
	ctx := context.Background()

	mems, err := c.Memories(ctx, "")
	if err != nil || len(mems) != 1 {
		t.Fatalf("Memories: len=%d err=%v", len(mems), err)
	}
	if mems[0].Metrics.ODataID != "/redfish/v1/Systems/Self/Memory/0/Metrics" {
		t.Fatalf("Memory.Metrics 引用未解析: %+v", mems[0].Metrics)
	}

	mm, err := c.MemoryMetrics(ctx, "", "0")
	if err != nil {
		t.Fatalf("MemoryMetrics: %v", err)
	}
	if mm.Type != "DRAM" || mm.TemperatureCelsius == nil || *mm.TemperatureCelsius != 45.5 {
		t.Fatalf("MemoryMetrics 字段解析错误: %+v", mm)
	}
	if mm.CorrectableECCError == nil || *mm.CorrectableECCError != 2 {
		t.Fatalf("CorrectableECCError 解析错误: %+v", mm.CorrectableECCError)
	}
}
