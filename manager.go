package redfish

import (
	"context"
	"encoding/json"
	"fmt"
)

// ---------- Manager BMC 管理器（手册 3.54–3.55） ----------

// ConsoleInfo 控制台服务信息
type ConsoleInfo struct {
	ServiceEnabled        *bool    `json:"ServiceEnabled"`
	MaxConcurrentSessions *int     `json:"MaxConcurrentSessions"`
	ConnectTypesSupported []string `json:"ConnectTypesSupported"`
}

// Manager BMC 管理器资源
type Manager struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ODataType    string `json:"@odata.type"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	Description  string `json:"Description"`

	ManagerType           string      `json:"ManagerType"` // BMC
	FirmwareVersion       string      `json:"FirmwareVersion"`
	Model                 string      `json:"Model"`
	UUID                  string      `json:"UUID"`
	ServiceEntryPointUUID string      `json:"ServiceEntryPointUUID"`
	PowerState            string      `json:"PowerState"`
	DateTime              string      `json:"DateTime"`
	DateTimeLocalOffset   string      `json:"DateTimeLocalOffset"`
	AutoDSTEnabled        *bool       `json:"AutoDSTEnabled"`
	SerialConsole         ConsoleInfo `json:"SerialConsole"`
	CommandShell          ConsoleInfo `json:"CommandShell"`
	GraphicalConsole      ConsoleInfo `json:"GraphicalConsole"`
	Status                Status      `json:"Status"`

	EthernetInterfaces Link `json:"EthernetInterfaces"`
	SerialInterfaces   Link `json:"SerialInterfaces"`
	NetworkProtocol    Link `json:"NetworkProtocol"`
	LogServices        Link `json:"LogServices"`
	VirtualMedia       Link `json:"VirtualMedia"`
	HostInterfaces     Link `json:"HostInterfaces"`

	Oem json.RawMessage `json:"Oem"`
}

// Manager 获取 BMC 管理器信息。managerID 空 → 自动发现（AST2600 为 "Self"）。
func (c *Client) Manager(ctx context.Context, managerID string) (*Manager, error) {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return nil, err
	}
	var m Manager
	if err := c.Get(ctx, "/redfish/v1/Managers/"+id, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ResetManager 重启 BMC 本身（不影响主机电源；202 → 任务 URI）
func (c *Client) ResetManager(ctx context.Context, managerID string) (string, error) {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return "", err
	}
	return c.runAction(ctx, fmt.Sprintf("/redfish/v1/Managers/%s/Actions/Manager.Reset", id),
		map[string]string{"ResetType": ResetForceRestart})
}

// SetManagerDateTime 设置 BMC 时间（RFC3339 带偏移，如 "2026-09-08T10:00:00+08:00"）
func (c *Client) SetManagerDateTime(ctx context.Context, managerID, dateTime string) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	return c.Patch(ctx, "/redfish/v1/Managers/"+id, map[string]string{"DateTime": dateTime}, nil)
}

// UpdateManagerConsole 开关串口/命令行/图形式控制台服务（204）
func (c *Client) UpdateManagerConsole(ctx context.Context, managerID string, serial, commandShell, graphical *bool) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if serial != nil {
		body["SerialConsole"] = map[string]bool{"ServiceEnabled": *serial}
	}
	if commandShell != nil {
		body["CommandShell"] = map[string]bool{"ServiceEnabled": *commandShell}
	}
	if graphical != nil {
		body["GraphicalConsole"] = map[string]bool{"ServiceEnabled": *graphical}
	}
	return c.Patch(ctx, "/redfish/v1/Managers/"+id, body, nil)
}

// ---------- ManagerNetworkProtocol 服务端口开关（手册 3.62） ----------

// ProtocolConfig 单个协议的启用/端口（NTPServers 仅 NTP 协议有值）
type ProtocolConfig struct {
	ProtocolEnabled *bool    `json:"ProtocolEnabled,omitempty"`
	Port            *int     `json:"Port,omitempty"`
	NTPServers      []string `json:"NTPServers,omitempty"`
}

// NetworkProtocol BMC 网络协议配置
type NetworkProtocol struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ODataType    string `json:"@odata.type"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	HostName     string `json:"HostName"`
	FQDN         string `json:"FQDN"`
	Status       Status `json:"Status"`

	HTTPS        ProtocolConfig `json:"HTTPS"`
	VirtualMedia ProtocolConfig `json:"VirtualMedia"`
	Telnet       ProtocolConfig `json:"Telnet"`
	SSDP         ProtocolConfig `json:"SSDP"`
	IPMI         ProtocolConfig `json:"IPMI"`
	SSH          ProtocolConfig `json:"SSH"`
	KVMIP        ProtocolConfig `json:"KVMIP"`
	NTP          ProtocolConfig `json:"NTP"`
	SNMP         ProtocolConfig `json:"SNMP"`
}

// NTPServers NTP 服务器地址列表
func (np *NetworkProtocol) NTPServers() []string { return np.NTP.NTPServers }

// NetworkProtocol 获取 BMC 服务协议配置
func (c *Client) NetworkProtocol(ctx context.Context, managerID string) (*NetworkProtocol, error) {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return nil, err
	}
	var np NetworkProtocol
	if err := c.Get(ctx, "/redfish/v1/Managers/"+id+"/NetworkProtocol", &np); err != nil {
		return nil, err
	}
	return &np, nil
}

// UpdateNetworkProtocol 更新服务端口/开关（202 任务：服务重启由任务监控）。
// 只更新传入的协议项。NTPServers 非空时写 NTP.NTPServers。
func (c *Client) UpdateNetworkProtocol(ctx context.Context, managerID string, protocols map[string]ProtocolConfig, ntpServers []string) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	body := map[string]any{}
	for name, pc := range protocols {
		body[name] = pc
	}
	if ntpServers != nil {
		if _, dup := body["NTP"]; dup {
			return fmt.Errorf("redfish: NTP 同时经 protocols 与 ntpServers 传入，二者只能选一")
		}
		body["NTP"] = map[string]any{"NTPServers": ntpServers}
	}
	return c.Patch(ctx, "/redfish/v1/Managers/"+id+"/NetworkProtocol", body, nil)
}

// ---------- SerialInterface 串口（手册 3.63–3.64） ----------

// SerialInterface BMC 串口实例（如 IPMI-SOL）
type SerialInterface struct {
	ODataContext     string `json:"@odata.context"`
	ODataID          string `json:"@odata.id"`
	ODataType        string `json:"@odata.type"`
	ID               string `json:"Id"`
	Name             string `json:"Name"`
	InterfaceEnabled *bool  `json:"InterfaceEnabled"`
	BitRate          string `json:"BitRate"` // 9600|19200|38400|57600|115200
	Parity           string `json:"Parity"`
	DataBits         string `json:"DataBits"`
	StopBits         string `json:"StopBits"`
	FlowControl      string `json:"FlowControl"`
	SignalType       string `json:"SignalType"`
	ConnectorType    string `json:"ConnectorType"`
}

// SerialInterfaces 列出 BMC 串口
func (c *Client) SerialInterfaces(ctx context.Context, managerID string) ([]*SerialInterface, error) {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Managers/"+id+"/SerialInterfaces")
	if err != nil {
		return nil, err
	}
	out := make([]*SerialInterface, 0, len(coll.Members))
	for _, m := range coll.Members {
		var si SerialInterface
		if err := c.Get(ctx, m.ODataID, &si); err != nil {
			return nil, err
		}
		out = append(out, &si)
	}
	return out, nil
}

// UpdateSerialInterface 更新串口配置（InterfaceEnabled/BitRate，204）
func (c *Client) UpdateSerialInterface(ctx context.Context, managerID, serialID string, enabled *bool, bitRate string) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if enabled != nil {
		body["InterfaceEnabled"] = *enabled
	}
	if bitRate != "" {
		body["BitRate"] = bitRate
	}
	return c.Patch(ctx, "/redfish/v1/Managers/"+id+"/SerialInterfaces/"+serialID, body, nil)
}
