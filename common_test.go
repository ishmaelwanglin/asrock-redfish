package redfish

import (
	"encoding/json"
	"testing"
)

// TestLinkUnmarshalObjectForm 对象形态（标准 Redfish）：@odata.id 正常解析
func TestLinkUnmarshalObjectForm(t *testing.T) {
	var l Link
	if err := json.Unmarshal([]byte(`{"@odata.id":"/redfish/v1/Systems/Self/Bios"}`), &l); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if l.ODataID != "/redfish/v1/Systems/Self/Bios" {
		t.Fatalf("ODataID = %q，期望 /redfish/v1/Systems/Self/Bios", l.ODataID)
	}
	if len(l.Items) != 0 {
		t.Fatalf("对象形态 Items 应为空，实际 %d 项", len(l.Items))
	}
}

// TestLinkUnmarshalArrayForm 数组形态（实测 AMI AST2600）：解析进 Items，ODataID 留空
func TestLinkUnmarshalArrayForm(t *testing.T) {
	raw := `[{"@odata.id":"/redfish/v1/Chassis/Self/PCIeDevices/00_00_08"},` +
		`{"@odata.id":"/redfish/v1/Chassis/Self/PCIeDevices/00_00_0A"}]`
	var l Link
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if l.ODataID != "" {
		t.Fatalf("数组形态 ODataID 应为空，实际 %q", l.ODataID)
	}
	if len(l.Items) != 2 {
		t.Fatalf("Items 数量 = %d，期望 2", len(l.Items))
	}
	if l.Items[1].ODataID != "/redfish/v1/Chassis/Self/PCIeDevices/00_00_0A" {
		t.Fatalf("Items[1].ODataID = %q", l.Items[1].ODataID)
	}
}

// TestLinkUnmarshalNullForm null 形态不报错
func TestLinkUnmarshalNullForm(t *testing.T) {
	var l Link
	if err := json.Unmarshal([]byte(`null`), &l); err != nil {
		t.Fatalf("解析 null 失败: %v", err)
	}
	if l.ODataID != "" || len(l.Items) != 0 {
		t.Fatalf("null 形态应解析为空，实际 %+v", l)
	}
}

// TestLinkUnmarshalInvalid 非对象非数组的形态报错，不静默吞错
func TestLinkUnmarshalInvalid(t *testing.T) {
	var l Link
	if err := json.Unmarshal([]byte(`"not-a-link"`), &l); err == nil {
		t.Fatal("非法形态应报错")
	}
}

// TestComputerSystemParseWithArrayPCIeDevices 回归：AST2600 把 PCIeDevices 返回为数组时，
// ComputerSystem 必须整体解析成功（否则 PowerState 读不到，表现为查电源状态必然失败）。
func TestComputerSystemParseWithArrayPCIeDevices(t *testing.T) {
	raw := `{
		"@odata.id":"/redfish/v1/Systems/Self",
		"Id":"Self",
		"PowerState":"On",
		"Model":"SP2C741D32G-2L+",
		"Status":{"Health":"OK","State":"Enabled"},
		"Boot":{"BootSourceOverrideEnabled":"Disabled","BootOptions":{"@odata.id":"/redfish/v1/Systems/Self/BootOptions"}},
		"PCIeDevices":[
			{"@odata.id":"/redfish/v1/Chassis/Self/PCIeDevices/00_00_08"},
			{"@odata.id":"/redfish/v1/Chassis/Self/PCIeDevices/00_03_00"}
		],
		"PCIeDevices@odata.count":2
	}`
	var cs ComputerSystem
	if err := json.Unmarshal([]byte(raw), &cs); err != nil {
		t.Fatalf("ComputerSystem 解析失败: %v", err)
	}
	if cs.PowerState != PowerOn {
		t.Fatalf("PowerState = %q，期望 %q", cs.PowerState, PowerOn)
	}
	if len(cs.PCIeDevices.Items) != 2 {
		t.Fatalf("PCIeDevices.Items 数量 = %d，期望 2", len(cs.PCIeDevices.Items))
	}
	if cs.Boot.BootOptions.ODataID != "/redfish/v1/Systems/Self/BootOptions" {
		t.Fatalf("Boot.BootOptions.ODataID = %q", cs.Boot.BootOptions.ODataID)
	}
}

// TestComputerSystemParseWithObjectPCIeDevices 其它厂商对象形态行为不变
func TestComputerSystemParseWithObjectPCIeDevices(t *testing.T) {
	raw := `{
		"Id":"1",
		"PowerState":"Off",
		"PCIeDevices":{"@odata.id":"/redfish/v1/Systems/1/PCIeDevices"}
	}`
	var cs ComputerSystem
	if err := json.Unmarshal([]byte(raw), &cs); err != nil {
		t.Fatalf("ComputerSystem 解析失败: %v", err)
	}
	if cs.PowerState != PowerOff {
		t.Fatalf("PowerState = %q，期望 %q", cs.PowerState, PowerOff)
	}
	if cs.PCIeDevices.ODataID != "/redfish/v1/Systems/1/PCIeDevices" {
		t.Fatalf("PCIeDevices.ODataID = %q", cs.PCIeDevices.ODataID)
	}
	if len(cs.PCIeDevices.Items) != 0 {
		t.Fatalf("对象形态 Items 应为空，实际 %d 项", len(cs.PCIeDevices.Items))
	}
}
