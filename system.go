package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// ---------- ComputerSystem（手册 3.77–3.78 + 3.95 SD + 附录 FixedBootOrder） ----------

// Boot 计算机系统引导配置（手册 Table 103）
type Boot struct {
	BootNext                     *string  `json:"BootNext"`
	BootSourceOverrideEnabled    *string  `json:"BootSourceOverrideEnabled"`
	BootSourceOverrideMode       *string  `json:"BootSourceOverrideMode"`
	BootSourceOverrideTarget     *string  `json:"BootSourceOverrideTarget"`
	UefiTargetBootSourceOverride *string  `json:"UefiTargetBootSourceOverride"`
	HttpBootUri                  *string  `json:"HttpBootUri"`
	BootOrder                    []string `json:"BootOrder"`
	AliasBootOrder               []string `json:"AliasBootOrder"`
	BootOptions                  Link     `json:"BootOptions"`
	Certificates                 Link     `json:"Certificates"`
}

// ProcessorSummary 处理器摘要
type ProcessorSummary struct {
	Count  *int   `json:"Count"`
	Model  string `json:"Model"`
	Status Status `json:"Status"`
}

// MemorySummary 内存摘要
type MemorySummary struct {
	TotalSystemMemoryGiB           *float64 `json:"TotalSystemMemoryGiB"`
	TotalSystemPersistentMemoryGiB *float64 `json:"TotalSystemPersistentMemoryGiB"`
	MemoryMirroring                string   `json:"MemoryMirroring"`
	Status                         Status   `json:"Status"`
}

// TrustedModule 可信模块（TPM/TCM）
type TrustedModule struct {
	FirmwareVersion  string `json:"FirmwareVersion"`
	InterfaceType    string `json:"InterfaceType"` // TPM1_2|TPM2_0|TCM1_0
	FirmwareVersion2 string `json:"FirmwareVersion2"`
}

// ComputerSystem 计算机系统资源（手册 Table 102）
type ComputerSystem struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ODataType    string `json:"@odata.type"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	Description  string `json:"Description"`

	AssetTag           string `json:"AssetTag"`
	BiosVersion        string `json:"BiosVersion"`
	HostName           string `json:"HostName"`
	Manufacturer       string `json:"Manufacturer"`
	Model              string `json:"Model"`
	SKU                string `json:"SKU"`
	SerialNumber       string `json:"SerialNumber"`
	PartNumber         string `json:"PartNumber"`
	SubModel           string `json:"SubModel"`
	UUID               string `json:"UUID"`
	SystemType         string `json:"SystemType"` // Physical|Composed
	PowerState         string `json:"PowerState"` // On|Off
	IndicatorLED       string `json:"IndicatorLED"`
	PowerRestorePolicy string `json:"PowerRestorePolicy"`

	Boot             Boot             `json:"Boot"`
	ProcessorSummary ProcessorSummary `json:"ProcessorSummary"`
	MemorySummary    MemorySummary    `json:"MemorySummary"`
	Status           Status           `json:"Status"`
	TrustedModules   []TrustedModule  `json:"TrustedModules"`

	// 子资源链接
	Bios                Link `json:"Bios"`
	EthernetInterfaces  Link `json:"EthernetInterfaces"`
	LogServices         Link `json:"LogServices"`
	Memory              Link `json:"Memory"`
	MemoryDomains       Link `json:"MemoryDomains"`
	NetworkInterfaces   Link `json:"NetworkInterfaces"`
	PCIeDevices         Link `json:"PCIeDevices"`
	Processors          Link `json:"Processors"`
	SecureBoot          Link `json:"SecureBoot"`
	SimpleStorage       Link `json:"SimpleStorage"`
	Storage             Link `json:"Storage"`
	USBControllers      Link `json:"USBControllers"`
	GraphicsControllers Link `json:"GraphicsControllers"`
	FruInfo             Link `json:"FruInfo"`

	Oem json.RawMessage `json:"Oem"`
}

// Systems 列出全部系统实例
func (c *Client) Systems(ctx context.Context) ([]string, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems")
	if err != nil {
		return nil, err
	}
	return coll.MemberIDs(), nil
}

// System 获取系统资源。systemID 空 → 自动发现（AST2600 为 "Self"）。
func (c *Client) System(ctx context.Context, systemID string) (*ComputerSystem, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	var s ComputerSystem
	if err := c.Get(ctx, "/redfish/v1/Systems/"+id, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SystemReset 电源操作（手册 3.78.4.3）。
// ResetType: On|ForceOff|GracefulShutdown|ForceRestart|PowerCycle。
// 返回任务 URI（202），空串表示无任务；调用方可选 WaitTask 等待完成。
func (c *Client) SystemReset(ctx context.Context, systemID, resetType string) (string, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return "", err
	}
	return c.runAction(ctx, fmt.Sprintf("/redfish/v1/Systems/%s/Actions/ComputerSystem.Reset", id),
		map[string]string{"ResetType": resetType})
}

// SystemResetAndWait 电源操作并等待任务终态
func (c *Client) SystemResetAndWait(ctx context.Context, systemID, resetType string) error {
	uri, err := c.SystemReset(ctx, systemID, resetType)
	if err != nil {
		return err
	}
	if uri == "" {
		return nil
	}
	_, err = c.WaitTask(ctx, uri, 0)
	return err
}

// SetSystemBootOnce 设置一次性引导（下次启动生效后自动失效）。
// target 如 BootPxe/BootCd/BootHdd；mode 可空（默认 UEFI）。
// 典型装机流程：SetSystemBootOnce(Pxe) → SystemReset(On/ForceRestart)。
func (c *Client) SetSystemBootOnce(ctx context.Context, systemID, target, mode string) error {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	if mode == "" {
		mode = BootModeUEFI
	}
	body := map[string]any{
		"Boot": map[string]any{
			"BootSourceOverrideEnabled": BootOverrideOnce,
			"BootSourceOverrideTarget":  target,
			"BootSourceOverrideMode":    mode,
		},
	}
	return c.Patch(ctx, "/redfish/v1/Systems/"+id, body, nil)
}

// ClearBootOverride 取消引导覆盖（BootSourceOverrideEnabled=Disabled）
func (c *Client) ClearBootOverride(ctx context.Context, systemID string) error {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	return c.Patch(ctx, "/redfish/v1/Systems/"+id,
		map[string]any{"Boot": map[string]any{"BootSourceOverrideEnabled": BootOverrideDisabled}}, nil)
}

// SetSystemBootOrder 设置持久启动顺序（AST2600 稳健路径，FBO 机型无效）
//
// v0.3.3 起行为变更（旧的直接 PATCH 主资源实现会「看起来成功但没落盘」）：
//   - 优先写 Settings(FutureState) 资源 /redfish/v1/Systems/<id>/SD
//     （AST2600 的 Boot 属性都在那里，直写主资源返回 400
//     "Support of this Operation for Boot Properties is moved to FutureState URI(...)"），
//     写 SD 时补齐 BootSourceOverrideTarget/Enabled/Mode 三件套（缺一 400）；
//   - 带 If-Match 乐观锁（AST2600 缺 If-Match 返回 428 Ami.1.0.PreconditionHeaderMissing）；
//   - SD 不可用或被拒时回退写主资源；
//   - 写完回读校验首位元素，固件 2xx 却不落盘时报错（不假装成功）。
//
// bootOrder 元素必须是引导项在顺序表里的**名字**（AST2600 形态 "Boot000E"），
// 不是 BootOptions 成员的 Id（"000E"）；用 ResolveBootEntry 从引导项反查元素名。
func (c *Client) SetSystemBootOrder(ctx context.Context, systemID string, bootOrder []string) error {
	if len(bootOrder) == 0 {
		return fmt.Errorf("BootOrder 不能为空")
	}
	for _, e := range bootOrder {
		if strings.TrimSpace(e) == "" {
			return fmt.Errorf("BootOrder 含空元素: %v", bootOrder)
		}
	}
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	sysPath := "/redfish/v1/Systems/" + id
	sdPath := c.bootSettingsPath(ctx, sysPath)
	return c.writeBootOrder(ctx, sysPath, sdPath, "", bootOrder, bootOrder[0])
}

// SetSystemAssetTag 设置资产标签（204）
func (c *Client) SetSystemAssetTag(ctx context.Context, systemID, tag string) error {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	return c.Patch(ctx, "/redfish/v1/Systems/"+id, map[string]string{"AssetTag": tag}, nil)
}

// SetSystemIndicatorLED 设置系统指示灯（PATCH 可写属性，手册 Table 102）。
// BMC 同步处理返回 204（任务 URI 为空串）；异步处理返回 202，任务 URI 取
// Location 头，其次取响应体为 Task 资源时的 @odata.id。
func (c *Client) SetSystemIndicatorLED(ctx context.Context, systemID, state string) (string, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return "", err
	}
	var location string
	var ar actionResponse
	status, err := c.do(ctx, apiRequest{
		method:      http.MethodPatch,
		path:        "/redfish/v1/Systems/" + id,
		body:        map[string]string{"IndicatorLED": state},
		outLocation: &location,
	}, &ar)
	if err != nil {
		return "", err
	}
	if status == http.StatusAccepted {
		if location != "" {
			return location, nil
		}
		if ar.ODataID != "" && ar.TaskState != "" {
			return ar.ODataID, nil
		}
	}
	return "", nil
}

// SetPowerRestorePolicy 设置来电恢复策略（AlwaysOn/AlwaysOff/LastState）
func (c *Client) SetPowerRestorePolicy(ctx context.Context, systemID, policy string) error {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	return c.Patch(ctx, "/redfish/v1/Systems/"+id, map[string]string{"PowerRestorePolicy": policy}, nil)
}

// ---------- 系统未来状态 SD（手册 3.95） ----------
// 引导配置走 /Systems/{id}/SD PATCH 下次启动生效（FBO 机型 Boot PATCH 会被阻止，
// 此时须改走 BIOS 属性，见 bios.go 的 FixedBootOrder 辅助函数）。

// SystemSD 获取系统未来状态视图
func (c *Client) SystemSD(ctx context.Context, systemID string) (*ComputerSystem, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	var s ComputerSystem
	if err := c.Get(ctx, "/redfish/v1/Systems/"+id+"/SD", &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SetSystemBootOrderSD 通过 SD 资源设置启动顺序（下次启动生效）
//
// v0.3.3 起走稳健路径：带 If-Match 乐观锁、补齐引导三件套
// （BootSourceOverrideTarget/Enabled/Mode，AST2600 缺一 400）、写完回读校验首位元素；
// SD 资源不存在时明确报错（不静默改写主资源，语义不同）。
func (c *Client) SetSystemBootOrderSD(ctx context.Context, systemID string, bootOrder []string) error {
	if len(bootOrder) == 0 {
		return fmt.Errorf("BootOrder 不能为空")
	}
	for _, e := range bootOrder {
		if strings.TrimSpace(e) == "" {
			return fmt.Errorf("BootOrder 含空元素: %v", bootOrder)
		}
	}
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	sysPath := "/redfish/v1/Systems/" + id
	sdPath := c.bootSettingsPath(ctx, sysPath)
	if sdPath == "" {
		return fmt.Errorf("该固件未暴露 Settings(FutureState) 资源，无法写 SD 引导顺序")
	}
	mode, err := c.bootOverrideMode(ctx, sysPath)
	if err != nil {
		return err
	}
	payload := map[string]any{"Boot": map[string]any{
		"BootOrder":                 bootOrder,
		"BootSourceOverrideTarget":  BootNone,
		"BootSourceOverrideEnabled": BootOverrideDisabled,
		"BootSourceOverrideMode":    mode,
	}}
	if err := c.PatchWithIfMatch(ctx, sdPath, payload); err != nil {
		return fmt.Errorf("写 SD 引导顺序失败: %w", err)
	}
	return c.verifyBootOrderAt(ctx, sdPath, bootOrder[0])
}

// ---------- 组合系统（手册 3.77 POST，SystemType=Composed） ----------

// CreateComposedSystem 通过组合 ResourceBlocks 创建新系统（201）
func (c *Client) CreateComposedSystem(ctx context.Context, name, hostName string, resourceBlockURIs []string) (*ComputerSystem, error) {
	blocks := make([]Link, 0, len(resourceBlockURIs))
	for _, u := range resourceBlockURIs {
		blocks = append(blocks, Link{ODataID: u})
	}
	body := map[string]any{
		"Name":     name,
		"HostName": hostName,
		"Links":    map[string]any{"ResourceBlocks": blocks},
	}
	var s ComputerSystem
	if _, err := c.Post(ctx, "/redfish/v1/Systems", body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
