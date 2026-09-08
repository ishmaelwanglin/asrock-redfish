package redfish

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// ---------- UpdateService 固件升级（手册 3.124–3.126） ----------

// 传输协议（手册 Table 145）
const (
	UpdateProtocolHTTP  = "HTTP"
	UpdateProtocolHTTPS = "HTTPS"
	UpdateProtocolFTP   = "FTP"
)

// 固件目标（手册 Table 148 Targets 允许值）
const (
	FirmwareBMC       = "/redfish/v1/UpdateService/FirmwareInventory/BMC"
	FirmwareBMCImage1 = "/redfish/v1/UpdateService/FirmwareInventory/BMCImage1"
	FirmwareBMCImage2 = "/redfish/v1/UpdateService/FirmwareInventory/BMCImage2"
	FirmwareBIOS      = "/redfish/v1/UpdateService/FirmwareInventory/BIOS"
	FirmwareME        = "/redfish/v1/UpdateService/FirmwareInventory/ME"
	FirmwareCPLD      = "/redfish/v1/UpdateService/FirmwareInventory/CPLD"
	FirmwareMMC       = "/redfish/v1/UpdateService/FirmwareInventory/MMC"
	FirmwareRAIDMSCC  = "/redfish/v1/UpdateService/FirmwareInventory/RAID_MSCC_CONTROLLER"
	FirmwareRAIDBRCM  = "/redfish/v1/UpdateService/FirmwareInventory/RAID_BRCM_CONTROLLER"
)

// PreserveConfiguration 升级保留配置项（手册 Table 142，全部 15 项）
type PreserveConfiguration struct {
	SDR, FRU, SEL, IPMI, Network, NTP, SNMP bool
	SSH, KVM, Authentication, Syslog        bool
	CMX, WEB, EXTLOG, REDFISH               bool
	AUTOMATION_ENGINE                       bool
}

// ToMap 序列化为 Oem.AMIUpdateService.PreserveConfiguration 请求体片段
func (p PreserveConfiguration) ToMap() map[string]bool {
	return map[string]bool{
		"SDR": p.SDR, "FRU": p.FRU, "SEL": p.SEL, "IPMI": p.IPMI,
		"Network": p.Network, "NTP": p.NTP, "SNMP": p.SNMP, "SSH": p.SSH,
		"KVM": p.KVM, "Authentication": p.Authentication, "Syslog": p.Syslog,
		"CMX": p.CMX, "WEB": p.WEB, "EXTLOG": p.EXTLOG, "REDFISH": p.REDFISH,
		"AUTOMATION_ENGINE": p.AUTOMATION_ENGINE,
	}
}

// AMIUpdateService AMI 升级扩展（FlashPercentage 进度等）
type AMIUpdateService struct {
	FlashPercentage *float64 `json:"FlashPercentage"`
	UpdateStatus    string   `json:"UpdateStatus"`
	UpdateTarget    string   `json:"UpdateTarget"`
}

// UpdateService 升级服务资源
type UpdateService struct {
	ODataContext         string `json:"@odata.context"`
	ODataID              string `json:"@odata.id"`
	ODataType            string `json:"@odata.type"`
	ID                   string `json:"Id"`
	Name                 string `json:"Name"`
	ServiceEnabled       *bool  `json:"ServiceEnabled"`
	MaxImageSizeBytes    *int64 `json:"MaxImageSizeBytes"`
	MultipartHttpPushUri string `json:"MultipartHttpPushUri"`
	FirmwareInventory    Link   `json:"FirmwareInventory"`
	Status               Status `json:"Status"`
	Oem                  struct {
		AMIUpdateService AMIUpdateService `json:"AMIUpdateService"`
	} `json:"Oem"`
}

// UpdateService 获取升级服务（MultipartHttpPushUri 为实际推送端点，通常 /redfish/v1/UpdateService/upload）
func (c *Client) UpdateService(ctx context.Context) (*UpdateService, error) {
	var us UpdateService
	if err := c.Get(ctx, "/redfish/v1/UpdateService", &us); err != nil {
		return nil, err
	}
	return &us, nil
}

// UpdateServiceProgress 当前刷写进度（FlashPercentage；无升级进行时为 nil/0）
func (c *Client) UpdateServiceProgress(ctx context.Context) (*AMIUpdateService, error) {
	us, err := c.UpdateService(ctx)
	if err != nil {
		return nil, err
	}
	return &us.Oem.AMIUpdateService, nil
}

// SetPreserveConfiguration 设置升级保留配置项（PATCH，204）
func (c *Client) SetPreserveConfiguration(ctx context.Context, p PreserveConfiguration) error {
	return c.Patch(ctx, "/redfish/v1/UpdateService", map[string]any{
		"Oem": map[string]any{
			"AMIUpdateService": map[string]any{"PreserveConfiguration": p.ToMap()},
		},
	}, nil)
}

// SimpleUpdate 从网络地址拉取固件升级（手册 3.124.4.3/4，202）。
// imageURI 携带协议前缀（http://、ftp://）；FTP 需账号密码。
// 返回任务 URI（UpdateService 的 202 响应中 Task.1.0.New 消息内）。
func (c *Client) SimpleUpdate(ctx context.Context, protocol, imageURI, user, password string) (string, error) {
	body := map[string]any{
		"TransferProtocol": protocol,
		"ImageURI":         imageURI,
	}
	if user != "" {
		body["User"] = user
	}
	if password != "" {
		body["Password"] = password
	}
	return c.runAction(ctx, "/redfish/v1/UpdateService/Actions/SimpleUpdate", body)
}

// UploadImage 本地镜像推送升级（MultipartHttpPush，手册 3.124.4.5，202）。
// targets 为固件目标 URI 列表（FirmwareBMC 等；nil 表示由 BMC 自行判断）。
// imageType: BMC|HPM|BIOS|PFR（OemParameters.ImageType）。
// 返回任务 URI。
func (c *Client) UploadImage(ctx context.Context, imageFile string, targets []string, imageType string) (string, error) {
	f, err := os.Open(imageFile)
	if err != nil {
		return "", fmt.Errorf("redfish: 打开镜像文件失败: %w", err)
	}
	defer f.Close()
	imageData, err := io.ReadAll(f)
	if err != nil {
		return "", fmt.Errorf("redfish: 读取镜像文件失败: %w", err)
	}
	return c.uploadImageBytes(ctx, imageData, targets, imageType)
}

func (c *Client) uploadImageBytes(ctx context.Context, imageData []byte, targets []string, imageType string) (string, error) {
	fields := []multipartField{
		{
			name:     "UpdateFile",
			filename: "image.bin",
			data:     imageData,
		},
	}
	if targets != nil {
		params, err := json.Marshal(map[string]any{"Targets": targets})
		if err != nil {
			return "", fmt.Errorf("redfish: 序列化 UpdateParameters 失败: %w", err)
		}
		fields = append(fields, multipartField{
			name: "UpdateParameters", filename: "parameters.json",
			contentType: "application/json", data: params,
		})
	}
	if imageType != "" {
		oem, err := json.Marshal(map[string]string{"ImageType": imageType})
		if err != nil {
			return "", fmt.Errorf("redfish: 序列化 OemParameters 失败: %w", err)
		}
		fields = append(fields, multipartField{
			name: "OemParameters", filename: "oem_parameters.json",
			contentType: "application/json", data: oem,
		})
	}
	var ar actionResponse
	if _, err := c.do(ctx, apiRequest{method: "POST", path: "/redfish/v1/UpdateService/upload", multipart: fields}, &ar); err != nil {
		return "", err
	}
	for _, m := range ar.Messages {
		if m.MessageID == "Task.1.0.New" && len(m.MessageArgs) > 0 {
			return m.MessageArgs[0], nil
		}
	}
	return "", nil
}

// UploadImageAndWait 推送镜像并轮询任务至终态；ctx 控制总超时。
// 升级期间 BMC 可能重启导致任务查询瞬时失败，此函数容忍连续查询失败
// （含网络错误与 HTTP 错误）约 30s，超出即判定升级异常。
func (c *Client) UploadImageAndWait(ctx context.Context, imageFile string, targets []string, imageType string) error {
	uri, err := c.UploadImage(ctx, imageFile, targets, imageType)
	if err != nil {
		return err
	}
	return c.waitTaskTolerant(ctx, uri)
}

// waitTaskTolerant 容忍 BMC 重启窗口的任务轮询（仅升级类长任务使用）。
// 对任意查询失败计数（网络错误与 HTTP 错误一视同仁），连续 15 次（约 30s）判失败。
func (c *Client) waitTaskTolerant(ctx context.Context, uri string) error {
	consecutiveFailures := 0
	for {
		var t Task
		err := c.Get(ctx, uri, &t)
		if err != nil {
			consecutiveFailures++
			if consecutiveFailures > 15 { // ~30s 连续失败判定升级异常
				return fmt.Errorf("redfish: 升级任务 %s 持续不可达: %w", uri, err)
			}
		} else {
			consecutiveFailures = 0
			if t.Done() {
				if !t.OK() {
					msg := ""
					if len(t.Messages) > 0 {
						msg = t.Messages[0].Message
					}
					return fmt.Errorf("redfish: 升级任务终态 %s/%s: %s", t.TaskState, t.TaskStatus, msg)
				}
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// FirmwareInventoryItem 固件清单条目
type FirmwareInventoryItem struct {
	ODataID    string `json:"@odata.id"`
	ID         string `json:"Id"` // BIOS|BMC|CPLD|...
	Name       string `json:"Name"`
	Version    string `json:"Version"`
	Updateable *bool  `json:"Updateable"`
}

// FirmwareInventory 列出全部固件版本
func (c *Client) FirmwareInventory(ctx context.Context) ([]*FirmwareInventoryItem, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/UpdateService/FirmwareInventory")
	if err != nil {
		return nil, err
	}
	out := make([]*FirmwareInventoryItem, 0, len(coll.Members))
	for _, m := range coll.Members {
		var fi FirmwareInventoryItem
		if err := c.Get(ctx, m.ODataID, &fi); err != nil {
			return nil, err
		}
		out = append(out, &fi)
	}
	return out, nil
}
