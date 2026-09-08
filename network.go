package redfish

import (
	"context"
	"fmt"
)

// ---------- EthernetInterface BMC 网络接口（手册 3.56–3.58） ----------
// 设置 BMC 管理 IP 的主入口：静态地址/DHCP 开关均在此。

// IPv4Address IPv4 地址条目
type IPv4Address struct {
	Address       string `json:"Address"`
	SubnetMask    string `json:"SubnetMask"`
	AddressOrigin string `json:"AddressOrigin"` // DHCP|Static
	Gateway       string `json:"Gateway"`
}

// IPv6Address IPv6 地址条目
type IPv6Address struct {
	Address       string `json:"Address"`
	PrefixLength  *int   `json:"PrefixLength"`
	AddressOrigin string `json:"AddressOrigin"` // LinkLocal|SLAAC|DHCP|Static
	AddressState  string `json:"AddressState"`  // Preferred|Deprecated|Tentative|Failed
}

// DHCPv4Config DHCPv4 行为配置
type DHCPv4Config struct {
	DHCPEnabled     *bool   `json:"DHCPEnabled"`
	UseDNSServers   *bool   `json:"UseDNSServers"`
	UseGateway      *bool   `json:"UseGateway"`
	UseDomainName   *bool   `json:"UseDomainName"`
	UseNTPServers   *bool   `json:"UseNTPServers"`
	UseStaticRoutes *bool   `json:"UseStaticRoutes"`
	FallbackAddress *string `json:"FallbackAddress"` // Static|AutoConfig|None
}

// DHCPv6Config DHCPv6 行为配置
type DHCPv6Config struct {
	OperatingMode  *string `json:"OperatingMode"` // Stateful|Stateless|Disabled
	UseDNSServers  *bool   `json:"UseDNSServers"`
	UseDomainName  *bool   `json:"UseDomainName"`
	UseNTPServers  *bool   `json:"UseNTPServers"`
	UseRapidCommit *bool   `json:"UseRapidCommit"`
}

// EthernetInterface 以太网接口（BMC 侧：bond0/eth0/eth1/usb0；主机侧：Systems/{id}/EthernetInterfaces）
type EthernetInterface struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ODataType    string `json:"@odata.type"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	Description  string `json:"Description"`

	InterfaceEnabled    *bool         `json:"InterfaceEnabled"`
	MACAddress          string        `json:"MACAddress"`
	PermanentMACAddress string        `json:"PermanentMACAddress"`
	LinkStatus          string        `json:"LinkStatus"` // LinkUp|NoLink|LinkDown
	SpeedMbps           *int          `json:"SpeedMbps"`
	AutoNeg             *bool         `json:"AutoNeg"`
	FullDuplex          *bool         `json:"FullDuplex"`
	MTUSize             *int          `json:"MTUSize"`
	HostName            string        `json:"HostName"`
	FQDN                string        `json:"FQDN"`
	NameServers         []string      `json:"NameServers"`
	IPv4Addresses       []IPv4Address `json:"IPv4Addresses"`
	IPv6Addresses       []IPv6Address `json:"IPv6Addresses"`
	IPv6DefaultGateway  string        `json:"IPv6DefaultGateway"`
	DHCPv4              DHCPv4Config  `json:"DHCPv4"`
	DHCPv6              DHCPv6Config  `json:"DHCPv6"`
	VLANs               Link          `json:"VLANs"`
	Status              Status        `json:"Status"`
}

// EthernetInterfaces 列出 BMC 侧以太网接口。managerID 空 → 自动发现。
// 主机侧接口在 Systems/{id}/EthernetInterfaces，见 HostEthernetInterfaces。
func (c *Client) EthernetInterfaces(ctx context.Context, managerID string) ([]*EthernetInterface, error) {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Managers/"+id+"/EthernetInterfaces")
	if err != nil {
		return nil, err
	}
	out := make([]*EthernetInterface, 0, len(coll.Members))
	for _, m := range coll.Members {
		var ei EthernetInterface
		if err := c.Get(ctx, m.ODataID, &ei); err != nil {
			return nil, err
		}
		out = append(out, &ei)
	}
	return out, nil
}

// EthernetInterface 获取单个 BMC 网络接口
func (c *Client) EthernetInterface(ctx context.Context, managerID, ifaceID string) (*EthernetInterface, error) {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return nil, err
	}
	var ei EthernetInterface
	if err := c.Get(ctx, "/redfish/v1/Managers/"+id+"/EthernetInterfaces/"+ifaceID, &ei); err != nil {
		return nil, err
	}
	return &ei, nil
}

// SetBMCStaticIPv4 设置 BMC 接口静态 IPv4（关闭 DHCPv4 → 写地址/掩码/网关，204）。
// 地址取值即生效参数；ifaceID 空 → 取第一个接口（bond0/eth0）。
func (c *Client) SetBMCStaticIPv4(ctx context.Context, managerID, ifaceID, ip, mask, gateway string) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	if ifaceID == "" {
		ifaceID, err = c.FirstMemberID(ctx, "/redfish/v1/Managers/"+id+"/EthernetInterfaces", "ethernet interface")
		if err != nil {
			return err
		}
	}
	body := map[string]any{
		"DHCPv4":        map[string]bool{"DHCPEnabled": false},
		"IPv4Addresses": []map[string]string{{"Address": ip, "SubnetMask": mask, "Gateway": gateway, "AddressOrigin": "Static"}},
	}
	return c.Patch(ctx, "/redfish/v1/Managers/"+id+"/EthernetInterfaces/"+ifaceID, body, nil)
}

// SetBMCDHCPv4 切换 BMC 接口 DHCPv4
func (c *Client) SetBMCDHCPv4(ctx context.Context, managerID, ifaceID string, enabled bool) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	if ifaceID == "" {
		ifaceID, err = c.FirstMemberID(ctx, "/redfish/v1/Managers/"+id+"/EthernetInterfaces", "ethernet interface")
		if err != nil {
			return err
		}
	}
	return c.Patch(ctx, "/redfish/v1/Managers/"+id+"/EthernetInterfaces/"+ifaceID,
		map[string]any{"DHCPv4": map[string]bool{"DHCPEnabled": enabled}}, nil)
}

// UpdateEthernetInterface 更新接口通用字段（InterfaceEnabled/HostName/NameServers 等，按需传）
func (c *Client) UpdateEthernetInterface(ctx context.Context, managerID, ifaceID string, body map[string]any) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	return c.Patch(ctx, "/redfish/v1/Managers/"+id+"/EthernetInterfaces/"+ifaceID, body, nil)
}

// ---------- VLAN（手册 3.29–3.31） ----------

// VLAN VLAN 接口
type VLAN struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ODataType    string `json:"@odata.type"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	VLANId       *int   `json:"VLANId"` // 0-4094
	VLANEnable   *bool  `json:"VLANEnable"`
	VLANPriority *int   `json:"VLANPriority"` // 0-7
}

// CreateVLAN 在接口下创建 VLAN（POST 集合，手册示例返回 204）
func (c *Client) CreateVLAN(ctx context.Context, baseVLANsURI string, vlanID int, enabled bool, priority int) error {
	body := map[string]any{
		"VLANId":       vlanID,
		"VLANEnable":   enabled,
		"VLANPriority": priority,
	}
	_, err := c.Post(ctx, baseVLANsURI, body, nil)
	return err
}

// DeleteVLAN 删除 VLAN（204）
func (c *Client) DeleteVLAN(ctx context.Context, vlanURI string) error {
	return c.Delete(ctx, vlanURI)
}

// ---------- HostInterface 主机带内接口（手册 3.59–3.61） ----------

// HostInterface 主机接口（BMC↔主机带内 Redfish 通道）
type HostInterface struct {
	ODataContext         string   `json:"@odata.context"`
	ODataID              string   `json:"@odata.id"`
	ODataType            string   `json:"@odata.type"`
	ID                   string   `json:"Id"`
	Name                 string   `json:"Name"`
	HostInterfaceType    string   `json:"HostInterfaceType"` // NetworkHostInterface
	InterfaceEnabled     *bool    `json:"InterfaceEnabled"`
	ExternallyAccessible *bool    `json:"ExternallyAccessible"`
	AuthenticationModes  []string `json:"AuthenticationModes"` // AuthNone|BasicAuth|RedfishSessionAuth|OemAuth
	Status               Status   `json:"Status"`
}

// HostInterfaces 列出主机接口
func (c *Client) HostInterfaces(ctx context.Context, managerID string) ([]*HostInterface, error) {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Managers/"+id+"/HostInterfaces")
	if err != nil {
		return nil, err
	}
	out := make([]*HostInterface, 0, len(coll.Members))
	for _, m := range coll.Members {
		var hi HostInterface
		if err := c.Get(ctx, m.ODataID, &hi); err != nil {
			return nil, err
		}
		out = append(out, &hi)
	}
	return out, nil
}

// UpdateHostInterface 更新主机接口（InterfaceEnabled 等，204）
func (c *Client) UpdateHostInterface(ctx context.Context, managerID, hostIfID string, body map[string]any) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	return c.Patch(ctx, fmt.Sprintf("/redfish/v1/Managers/%s/HostInterfaces/%s", id, hostIfID), body, nil)
}
