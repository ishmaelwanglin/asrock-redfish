package redfish

import (
	"context"
	"os"
)

// ---------- AMI OEM 扩展动作与资源 ----------

// UploadCABundle 上传 CA Bundle（OEM Action，手册 Table 150）。
// pem 内容为全部 RootCA 与中间证书（HTTPS 升级拉取时的 SSL 校验依据）。
func (c *Client) UploadCABundle(ctx context.Context, pemFile string) error {
	data, err := os.ReadFile(pemFile)
	if err != nil {
		return err
	}
	fields := []multipartField{{
		name: "ca_bundle", filename: "ca_bundle.pem",
		contentType: "application/x-pem-file", data: data,
	}}
	_, err = c.do(ctx, apiRequest{
		method: "POST", path: "/redfish/v1/UpdateService/Actions/Oem/UpdateService.UploadCABundle",
		multipart: fields,
	}, nil)
	return err
}

// ResetRedfishDB 重建 BMC Redfish 数据库（OEM Action #AMIManager.RedfishDBReset）
func (c *Client) ResetRedfishDB(ctx context.Context, managerID string) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	_, err = c.Post(ctx, "/redfish/v1/Managers/"+id+"/Actions/Oem/AMIManager.RedfishDBReset", map[string]any{}, nil)
	return err
}

// SetPowerSaveMode 设置 BMC 省电模式（OEM Action #AMIManager.RedfishPowerSaveMode，enabled 如 "Enable"/"Disable"）
func (c *Client) SetPowerSaveMode(ctx context.Context, managerID, mode string) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	_, err = c.Post(ctx, "/redfish/v1/Managers/"+id+"/Actions/Oem/AMIManager.RedfishPowerSaveMode",
		map[string]string{"PowerSaveMode": mode}, nil)
	return err
}

// ConfigureCDInstance 设置虚拟媒体 CD 实例数（OEM Action #AMIVirtualMedia.ConfigureCDInstance）
func (c *Client) ConfigureCDInstance(ctx context.Context, managerID string, instances int) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	_, err = c.Post(ctx, "/redfish/v1/Managers/"+id+"/Actions/Oem/AMIVirtualMedia.ConfigureCDInstance",
		map[string]int{"CDInstances": instances}, nil)
	return err
}

// EnableRMedia 开关远程媒体功能（OEM Action #AMIVirtualMedia.EnableRMedia）
func (c *Client) EnableRMedia(ctx context.Context, managerID string, enabled bool) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	_, err = c.Post(ctx, "/redfish/v1/Managers/"+id+"/Actions/Oem/AMIVirtualMedia.EnableRMedia",
		map[string]bool{"RMedia": enabled}, nil)
	return err
}

// ---------- Oem/Ami/Configurations 根级配置（手册 3.67） ----------

// AMIConfigurations AMI 根级配置
type AMIConfigurations struct {
	ODataContext            string  `json:"@odata.context"`
	ODataID                 string  `json:"@odata.id"`
	ID                      string  `json:"Id"`
	CertificateAuthorityURL *string `json:"CertificateAuthorityUrl"`
}

// AMIConfigurations 获取 CA 证书 URL 配置
func (c *Client) AMIConfigurations(ctx context.Context) (*AMIConfigurations, error) {
	var cfg AMIConfigurations
	if err := c.Get(ctx, "/redfish/v1/Oem/Ami/Configurations", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SetCertificateAuthorityURL 设置 Redfish 客户端 CA 证书下载地址。
// 优先于 DHCP option 248（REDFISH_CA_CERTIFICATE）；传空串恢复使用 DHCP option。
// 注意：成功后 BMC web 服务会重启数秒不可达。
func (c *Client) SetCertificateAuthorityURL(ctx context.Context, url string) error {
	var v any
	if url != "" {
		v = url
	} // 空串序列化为 null，恢复 DHCP option 语义
	return c.Patch(ctx, "/redfish/v1/Oem/Ami/Configurations", map[string]any{"CertificateAuthorityUrl": v}, nil)
}

// ---------- InventoryData Status（手册 3.68，只读） ----------

// InventoryDataStatus BIOS 清单文件处理状态
type InventoryDataStatus struct {
	ODataContext  string `json:"@odata.context"`
	ODataID       string `json:"@odata.id"`
	ID            string `json:"Id"`
	InventoryData struct {
		Status           string   `json:"Status"` // BootInProgress|Ready|Queued|In-Progress|Failed|Completed
		LastModifiedTime string   `json:"LastModifiedTime"`
		ProcessingTime   *float64 `json:"ProcessingTime"`
		Messages         []string `json:"Messages"`
		DeletedModules   []string `json:"DeletedModules"` // CPU|DIMM|PCIE|CERTIFICATES|SECURBOOT
	} `json:"InventoryData"`
}

// InventoryDataStatus 获取 BIOS 清单处理状态（盘点数据是否就绪的判断依据）
func (c *Client) InventoryDataStatus(ctx context.Context) (*InventoryDataStatus, error) {
	var s InventoryDataStatus
	if err := c.Get(ctx, "/redfish/v1/Oem/Ami/InventoryData/Status", &s); err != nil {
		return nil, err
	}
	return &s, nil
}
