package redfish

import (
	"context"
	"fmt"
)

// ---------- NetworkDeviceFunction 网络设备功能（手册 3.26–3.28） ----------
// 网卡 PF 级配置：BootMode（PXE/iSCSI/FC）、iSCSI 引导参数、FC WWPN。
// 实例本身只读，修改走 /SD 未来状态资源（下次启动生效）。

// BootMode 枚举（手册 3.27）
const (
	NDFBootDisabled = "Disabled"
	NDFBootPXE      = "PXE"
	NDFBootiSCSI    = "iSCSI"
	NDFBootFC       = "FibreChannel"
	NDFBootFCoE     = "FibreChannelOverEthernet"
)

// iSCSIBootTarget iSCSI 引导目标（Primary/Secondary 各一）。只经 ToMap 展开，不反序列化。
type iSCSIBootTarget struct {
	TargetName      string
	TargetIPAddress string
	TargetTCPPort   *int
	LUN             *int
	VLANEnable      *bool
	VLANId          *int
	DNS             string
}

// iSCSIBootSettings iSCSI 引导完整配置（手册 3.27 属性表 + 3.28 PATCH 体）。
// 只经 ToMap 逐字段展开为请求体（Primary/Secondary 前缀命名），不走 encoding/json，故不带 json tag。
type iSCSIBootSettings struct {
	AuthenticationMethod       string // None|CHAP|MutualCHAP
	IPAddressType              string // IPv4|IPv6
	IPMaskDNSViaDHCP           *bool
	TargetInfoViaDHCP          *bool
	InitiatorName              string
	InitiatorIPAddress         string
	InitiatorNetmask           string
	InitiatorDefaultGateway    string
	RouterAdvertisementEnabled *bool
	PrimaryTarget              iSCSIBootTarget
	SecondaryTarget            iSCSIBootTarget
	CHAPUsername               string
	CHAPSecret                 string
	MutualCHAPUsername         string
	MutualCHAPSecret           string
}

// ToMap 展开为 Redfish iSCSIBoot 对象（Primary/Secondary 字段前缀命名）
func (s iSCSIBootSettings) ToMap() map[string]any {
	m := map[string]any{}
	put := func(k string, v any) {
		switch t := v.(type) {
		case string:
			if t != "" {
				m[k] = t
			}
		case *bool:
			if t != nil {
				m[k] = *t
			}
		case *int:
			if t != nil {
				m[k] = *t
			}
		}
	}
	put("AuthenticationMethod", s.AuthenticationMethod)
	put("IPAddressType", s.IPAddressType)
	put("IPMaskDNSViaDHCP", s.IPMaskDNSViaDHCP)
	put("TargetInfoViaDHCP", s.TargetInfoViaDHCP)
	put("InitiatorName", s.InitiatorName)
	put("InitiatorIPAddress", s.InitiatorIPAddress)
	put("InitiatorNetmask", s.InitiatorNetmask)
	put("InitiatorDefaultGateway", s.InitiatorDefaultGateway)
	put("RouterAdvertisementEnabled", s.RouterAdvertisementEnabled)
	prefixes := []struct {
		p string
		t iSCSIBootTarget
	}{{"Primary", s.PrimaryTarget}, {"Secondary", s.SecondaryTarget}}
	for _, pr := range prefixes {
		put(pr.p+"TargetName", pr.t.TargetName)
		put(pr.p+"TargetIPAddress", pr.t.TargetIPAddress)
		put(pr.p+"TargetTCPPort", pr.t.TargetTCPPort)
		put(pr.p+"LUN", pr.t.LUN)
		put(pr.p+"VLANEnable", pr.t.VLANEnable)
		put(pr.p+"VLANId", pr.t.VLANId)
		put(pr.p+"DNS", pr.t.DNS)
	}
	put("CHAPUsername", s.CHAPUsername)
	put("CHAPSecret", s.CHAPSecret)
	put("MutualCHAPUsername", s.MutualCHAPUsername)
	put("MutualCHAPSecret", s.MutualCHAPSecret)
	return m
}

// FibreChannelSettings FC 引导配置
type FibreChannelSettings struct {
	WWPN                  string `json:"WWPN,omitempty"`
	WWNN                  string `json:"WWNN,omitempty"`
	WWNSource             string `json:"WWNSource,omitempty"` // ConfiguredLocally|ProvidedByFabric
	FCoELocalVLANId       *int   `json:"FCoELocalVLANId,omitempty"`
	AllowFIPVLANDiscovery *bool  `json:"AllowFIPVLANDiscovery,omitempty"`
}

// NetworkDeviceFunction 网络设备功能实例
type NetworkDeviceFunction struct {
	ODataContext   string `json:"@odata.context"`
	ODataID        string `json:"@odata.id"`
	ODataType      string `json:"@odata.type"`
	ID             string `json:"Id"`
	Name           string `json:"Name"`
	NetDevFuncType string `json:"NetDevFuncType"` // Ethernet|iSCSI|FibreChannel|Disabled|...
	DeviceEnabled  *bool  `json:"DeviceEnabled"`
	BootMode       string `json:"BootMode"` // Disabled|PXE|iSCSI|FibreChannel|FibreChannelOverEthernet
	Ethernet       struct {
		PermanentMACAddress string `json:"PermanentMACAddress"`
		MACAddress          string `json:"MACAddress"`
		MTUSize             *int   `json:"MTUSize"`
		VLANs               Link   `json:"VLANs"`
	} `json:"Ethernet"`
	MaxVirtualFunctions     *int                 `json:"MaxVirtualFunctions"`
	VirtualFunctionsEnabled *bool                `json:"VirtualFunctionsEnabled"`
	Status                  Status               `json:"Status"`
	ISCSIBoot               map[string]any       `json:"iSCSIBoot"` // 读取侧：当前 iSCSI 引导配置
	FibreChannel            FibreChannelSettings `json:"FibreChannel"`
}

// NetworkDeviceFunctions 列出网卡的全部网络设备功能。
// 两个宿主路径：Chassis/{id}/NetworkAdapters/{nic}/NetworkDeviceFunctions 或
// Systems/{id}/NetworkInterfaces/{nic}/NetworkDeviceFunctions；传完整集合 URI。
func (c *Client) NetworkDeviceFunctions(ctx context.Context, collectionURI string) ([]*NetworkDeviceFunction, error) {
	coll, err := c.CollectionAll(ctx, collectionURI)
	if err != nil {
		return nil, err
	}
	out := make([]*NetworkDeviceFunction, 0, len(coll.Members))
	for _, m := range coll.Members {
		var f NetworkDeviceFunction
		if err := c.Get(ctx, m.ODataID, &f); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, nil
}

// UpdateNetworkDeviceFunctionSD 修改网络设备功能未来状态（PATCH /SD，204，下次启动生效）。
// body 支持 BootMode / Ethernet.MTUSize / iSCSIBoot / FibreChannel 片段。
func (c *Client) UpdateNetworkDeviceFunctionSD(ctx context.Context, functionURI string, body map[string]any) error {
	return c.Patch(ctx, functionURI+"/SD", body, nil)
}

// SetNetworkDeviceBootMode 设置网卡引导模式（SD，下次启动生效）
func (c *Client) SetNetworkDeviceBootMode(ctx context.Context, functionURI, bootMode string) error {
	return c.UpdateNetworkDeviceFunctionSD(ctx, functionURI, map[string]any{"BootMode": bootMode})
}

// ConfigureiSCSIBoot 配置 iSCSI 引导（SD，手册 3.28.4.2 请求体结构，204）
func (c *Client) ConfigureiSCSIBoot(ctx context.Context, functionURI string, settings iSCSIBootSettings) error {
	return c.UpdateNetworkDeviceFunctionSD(ctx, functionURI, map[string]any{"iSCSIBoot": settings.ToMap()})
}

// ---------- VLAN SD 修改（手册 3.31） ----------

// UpdateVLAN 修改 VLAN（PATCH VLANs/{id}/SD，下次启动生效，204）
func (c *Client) UpdateVLAN(ctx context.Context, vlanURI string, vlanID *int, enabled *bool, priority *int) error {
	body := map[string]any{}
	if vlanID != nil {
		body["VLANId"] = *vlanID
	}
	if enabled != nil {
		body["VLANEnable"] = *enabled
	}
	if priority != nil {
		body["VLANPriority"] = *priority
	}
	return c.Patch(ctx, vlanURI+"/SD", body, nil)
}

// ---------- Port 端口（手册 3.32–3.33） ----------
// 三个宿主：NetworkAdapters/{nic}/Ports、GraphicsControllers/{gc}/Ports、USBControllers/{uc}/Ports。
// Port.Ethernet.LLDPReceive 携带上联交换机 LLDP 信息。

// LLDPInfo LLDP 接收信息
type LLDPInfo struct {
	ChassisID        string `json:"ChassisId"`
	ChassisIDSubtype string `json:"ChassisIdSubtype"`
	PortID           string `json:"PortId"`
	PortIDSubtype    string `json:"PortIdSubtype"`
}

// Port 端口实例
type Port struct {
	ODataContext          string   `json:"@odata.context"`
	ODataID               string   `json:"@odata.id"`
	ODataType             string   `json:"@odata.type"`
	ID                    string   `json:"Id"`
	Name                  string   `json:"Name"`
	PortID                string   `json:"PortId"` // 物理端口丝印标签
	PortProtocol          string   `json:"PortProtocol"`
	LinkNetworkTechnology string   `json:"LinkNetworkTechnology"`
	LinkState             string   `json:"LinkState"`
	LinkStatus            string   `json:"LinkStatus"` // LinkUp|LinkDown|NoLink
	CurrentSpeedGbps      *float64 `json:"CurrentSpeedGbps"`
	MaxSpeedGbps          *float64 `json:"MaxSpeedGbps"`
	ActiveWidth           *int     `json:"ActiveWidth"`
	SignalDetected        *bool    `json:"SignalDetected"`
	InterfaceEnabled      *bool    `json:"InterfaceEnabled"`
	MaxFrameSize          *int     `json:"MaxFrameSize"`
	Ethernet              struct {
		FlowControlConfiguration string   `json:"FlowControlConfiguration"`
		FlowControlStatus        string   `json:"FlowControlStatus"`
		WakeOnLANEnabled         *bool    `json:"WakeOnLANEnabled"`
		EEEEnabled               *bool    `json:"EEEEnabled"`
		AssociatedMACAddresses   []string `json:"AssociatedMACAddresses"`
		LLDPReceive              LLDPInfo `json:"LLDPReceive"`
		LLDPTransmit             LLDPInfo `json:"LLDPTransmit"`
	} `json:"Ethernet"`
	LinkConfiguration struct {
		AutoSpeedNegotiationCapable *bool `json:"AutoSpeedNegotiationCapable"`
		AutoSpeedNegotiationEnabled *bool `json:"AutoSpeedNegotiationEnabled"`
	} `json:"LinkConfiguration"`
	Status Status `json:"Status"`
}

// Ports 列出端口集合（传 Ports 集合 URI）
func (c *Client) Ports(ctx context.Context, portsCollectionURI string) ([]*Port, error) {
	coll, err := c.CollectionAll(ctx, portsCollectionURI)
	if err != nil {
		return nil, err
	}
	out := make([]*Port, 0, len(coll.Members))
	for _, m := range coll.Members {
		var p Port
		if err := c.Get(ctx, m.ODataID, &p); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, nil
}

// NetworkAdapterPorts 网卡端口便捷入口（含 LLDP 上联信息）
func (c *Client) NetworkAdapterPorts(ctx context.Context, chassisID, adapterID string) ([]*Port, error) {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return nil, err
	}
	return c.Ports(ctx, fmt.Sprintf("/redfish/v1/Chassis/%s/NetworkAdapters/%s/Ports", id, adapterID))
}

// ---------- PCIeFunction（手册 3.36–3.37，只读） ----------

// PCIeFunction PCIe 功能实例
type PCIeFunction struct {
	ODataContext      string `json:"@odata.context"`
	ODataID           string `json:"@odata.id"`
	ID                string `json:"Id"`
	Name              string `json:"Name"`
	FunctionID        *int   `json:"FunctionId"`
	FunctionType      string `json:"FunctionType"` // Physical|Virtual
	DeviceClass       string `json:"DeviceClass"`  // NetworkController|...
	VendorID          string `json:"VendorId"`     // 如 0x8086
	DeviceID          string `json:"DeviceId"`     // 如 0x1563
	ClassCode         string `json:"ClassCode"`
	RevisionID        string `json:"RevisionId"`
	SubsystemID       string `json:"SubsystemId"`
	SubsystemVendorID string `json:"SubsystemVendorId"`
	Status            Status `json:"Status"`
}

// PCIeFunctions 列出 PCIe 设备的全部功能
func (c *Client) PCIeFunctions(ctx context.Context, chassisID, pcieDeviceID string) ([]*PCIeFunction, error) {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, fmt.Sprintf("/redfish/v1/Chassis/%s/PCIeDevices/%s/PCIeFunctions", id, pcieDeviceID))
	if err != nil {
		return nil, err
	}
	out := make([]*PCIeFunction, 0, len(coll.Members))
	for _, m := range coll.Members {
		var f PCIeFunction
		if err := c.Get(ctx, m.ODataID, &f); err != nil {
			return nil, err
		}
		out = append(out, &f)
	}
	return out, nil
}
