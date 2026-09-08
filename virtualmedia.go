package redfish

import (
	"context"
	"encoding/json"
	"fmt"
)

// ---------- VirtualMedia 虚拟媒体（手册 3.65–3.66） ----------
// 挂载 ISO/NFS 镜像到虚拟光驱，配合一次性 Cd 引导完成装机。

// 传输协议枚举（手册 Table 83）
const (
	TransferNFS   = "NFS"
	TransferCIFS  = "CIFS"
	TransferHTTPS = "HTTPS"
	TransferOEM   = "OEM" // AMI 扩展：CIFS 加密（SMBE）
)

// VirtualMedia 虚拟媒体实例（CD1–CD4）
type VirtualMedia struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ODataType    string `json:"@odata.type"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`

	Image                string          `json:"Image"`
	ImageName            string          `json:"ImageName"`
	ConnectedVia         string          `json:"ConnectedVia"` // NotConnected|URI|Applet|Oem
	Inserted             *bool           `json:"Inserted"`
	WriteProtected       *bool           `json:"WriteProtected"`
	MediaTypes           []string        `json:"MediaTypes"` // CD
	TransferProtocolType string          `json:"TransferProtocolType"`
	TransferMethod       string          `json:"TransferMethod"` // Stream
	UserName             string          `json:"UserName"`
	Oem                  json.RawMessage `json:"Oem"`
}

// RedirectionStatus AMI OEM 重定向状态（Oem.Ami.RedirectionStatus）。无 Oem 字段时为空串。
func (vm *VirtualMedia) RedirectionStatus() (string, error) {
	if len(vm.Oem) == 0 {
		return "", nil
	}
	var oem struct {
		Ami struct {
			RedirectionStatus string `json:"RedirectionStatus"`
			BoostMode         *bool  `json:"BoostMode"`
		} `json:"Ami"`
	}
	if err := json.Unmarshal(vm.Oem, &oem); err != nil {
		return "", fmt.Errorf("redfish: 解析 VirtualMedia Oem 字段失败: %w", err)
	}
	return oem.Ami.RedirectionStatus, nil
}

// VirtualMediaList 列出虚拟媒体实例（CD1–CD4）
func (c *Client) VirtualMediaList(ctx context.Context, managerID string) ([]string, error) {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Managers/"+id+"/VirtualMedia")
	if err != nil {
		return nil, err
	}
	return coll.MemberIDs(), nil
}

// VirtualMedia 获取单个虚拟媒体实例
func (c *Client) VirtualMedia(ctx context.Context, managerID, mediaID string) (*VirtualMedia, error) {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return nil, err
	}
	var vm VirtualMedia
	if err := c.Get(ctx, "/redfish/v1/Managers/"+id+"/VirtualMedia/"+mediaID, &vm); err != nil {
		return nil, err
	}
	return &vm, nil
}

// InsertMediaOptions 挂载参数（手册 Table 83 InsertMedia）
type InsertMediaOptions struct {
	Image                string // 镜像 URI：CIFS 如 //host/share/x.iso；NFS 如 nfs://host/path/x.iso；HTTPS 如 https://host/x.iso
	TransferProtocolType string // NFS|CIFS|HTTPS|OEM
	UserName             string // CIFS/OEM/HTTPS 必填
	Password             string // CIFS/OEM/HTTPS 必填
	WriteProtected       *bool
	Inserted             *bool
}

// InsertMedia 挂载远程镜像（202 → 任务 URI）。
// 装机典型链路：InsertMedia(ISO, CIFS) → SetSystemBootOnce(BootCd) → SystemReset(On)。
func (c *Client) InsertMedia(ctx context.Context, managerID, mediaID string, opts InsertMediaOptions) (string, error) {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"Image":                opts.Image,
		"TransferProtocolType": opts.TransferProtocolType,
	}
	if opts.UserName != "" {
		body["UserName"] = opts.UserName
	}
	if opts.Password != "" {
		body["Password"] = opts.Password
	}
	if opts.WriteProtected != nil {
		body["WriteProtected"] = *opts.WriteProtected
	}
	if opts.Inserted != nil {
		body["Inserted"] = *opts.Inserted
	}
	return c.runAction(ctx, "/redfish/v1/Managers/"+id+"/VirtualMedia/"+mediaID+"/Actions/VirtualMedia.InsertMedia", body)
}

// InsertMediaAndWait 挂载镜像并等待任务完成
func (c *Client) InsertMediaAndWait(ctx context.Context, managerID, mediaID string, opts InsertMediaOptions) error {
	uri, err := c.InsertMedia(ctx, managerID, mediaID, opts)
	if err != nil {
		return err
	}
	if uri == "" {
		return nil
	}
	_, err = c.WaitTask(ctx, uri, 0)
	return err
}

// EjectMedia 弹出镜像（204，同步完成）
func (c *Client) EjectMedia(ctx context.Context, managerID, mediaID string) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	_, err = c.Post(ctx, "/redfish/v1/Managers/"+id+"/VirtualMedia/"+mediaID+"/Actions/VirtualMedia.EjectMedia", map[string]any{}, nil)
	return err
}

// SetVirtualMediaBoostMode 开关 AMI 媒体加速（PATCH Oem.Ami.BoostMode，204）
func (c *Client) SetVirtualMediaBoostMode(ctx context.Context, managerID, mediaID string, boost bool) error {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return err
	}
	return c.Patch(ctx, "/redfish/v1/Managers/"+id+"/VirtualMedia/"+mediaID,
		map[string]any{"Oem": map[string]any{"Ami": map[string]bool{"BoostMode": boost}}}, nil)
}
