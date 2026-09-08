package redfish

import (
	"context"
)

// ---------- Storage 存储/RAID（手册 3.101–3.109） ----------

// RAID 类型（手册 3.108：仅 RAID0/1/5/6）
const (
	RAID0 = "RAID0"
	RAID1 = "RAID1"
	RAID5 = "RAID5"
	RAID6 = "RAID6"
)

// 条带大小枚举
const (
	Strip64K  = "64K"
	Strip128K = "128K"
	Strip256K = "256K"
	Strip512K = "512K"
	Strip1MB  = "1MB"
)

// StorageControllerInfo 内嵌控制器摘要（Storage.StorageControllers[]）
type StorageControllerInfo struct {
	MemberID                     string   `json:"MemberId"`
	Name                         string   `json:"Name"`
	Manufacturer                 string   `json:"Manufacturer"`
	Model                        string   `json:"Model"`
	SerialNumber                 string   `json:"SerialNumber"`
	FirmwareVersion              string   `json:"FirmwareVersion"`
	SpeedGbps                    *float64 `json:"SpeedGbps"`
	SupportedControllerProtocols []string `json:"SupportedControllerProtocols"`
	SupportedDeviceProtocols     []string `json:"SupportedDeviceProtocols"`
	SupportedRAIDTypes           []string `json:"SupportedRAIDTypes"`
	Status                       Status   `json:"Status"`
}

// Storage 存储子系统
type Storage struct {
	ODataContext       string                  `json:"@odata.context"`
	ODataID            string                  `json:"@odata.id"`
	ODataType          string                  `json:"@odata.type"`
	ID                 string                  `json:"Id"`
	Name               string                  `json:"Name"`
	Drives             []Link                  `json:"Drives"`
	Volumes            Link                    `json:"Volumes"`
	Controllers        Link                    `json:"Controllers"`
	StorageControllers []StorageControllerInfo `json:"StorageControllers"`
	Status             Status                  `json:"Status"`
}

// Storages 列出系统存储子系统 URI
func (c *Client) Storages(ctx context.Context, systemID string) ([]string, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+id+"/Storage")
	if err != nil {
		return nil, err
	}
	return coll.MemberIDs(), nil
}

// Storage 获取存储子系统详情（含控制器与盘列表）
func (c *Client) Storage(ctx context.Context, systemID, storageID string) (*Storage, error) {
	sysID, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	var s Storage
	if err := c.Get(ctx, "/redfish/v1/Systems/"+sysID+"/Storage/"+storageID, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// ---------- Drive 物理盘（手册 3.107，只读） ----------

// Drive 物理盘
type Drive struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ODataType    string `json:"@odata.type"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`

	Manufacturer                  string `json:"Manufacturer"`
	Model                         string `json:"Model"`
	SerialNumber                  string `json:"SerialNumber"`
	PartNumber                    string `json:"PartNumber"`
	Revision                      string `json:"Revision"`
	CapacityBytes                 *int64 `json:"CapacityBytes"`
	BlockSizeBytes                *int   `json:"BlockSizeBytes"`
	Protocol                      string `json:"Protocol"`        // NVMe|SATA|SAS|...
	MediaType                     string `json:"MediaType"`       // HDD|SSD|SMR
	StatusIndicator               string `json:"StatusIndicator"` // OK|Fail|Rebuild|Hotspare|InACriticalArray|...
	IndicatorLED                  string `json:"IndicatorLED"`
	RotationSpeedRPM              *int   `json:"RotationSpeedRPM"`
	FailurePredicted              *bool  `json:"FailurePredicted"`
	PredictedMediaLifeLeftPercent *int   `json:"PredictedMediaLifeLeftPercent"`
	Status                        Status `json:"Status"`
	Links                         struct {
		Volumes []Link `json:"Volumes"`
	} `json:"Links"`
}

// Drives 列出存储子系统全部物理盘
func (c *Client) Drives(ctx context.Context, systemID, storageID string) ([]*Drive, error) {
	sysID, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+sysID+"/Storage/"+storageID+"/Drives")
	if err != nil {
		return nil, err
	}
	out := make([]*Drive, 0, len(coll.Members))
	for _, m := range coll.Members {
		var d Drive
		if err := c.Get(ctx, m.ODataID, &d); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, nil
}

// ---------- Volume 逻辑卷（手册 3.108–3.109） ----------

// Volume 逻辑卷/RAID 卷
type Volume struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ODataType    string `json:"@odata.type"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`

	CapacityBytes     *int64 `json:"CapacityBytes"`
	VolumeType        string `json:"VolumeType"` // RawDevice|NonRedundant|Mirrored|StripedWithParity|...
	RAIDType          string `json:"RAIDType"`   // RAID0|RAID1|RAID5|RAID6
	StripSizeBytes    *int64 `json:"StripSizeBytes"`
	DisplayName       string `json:"DisplayName"`
	LogicalUnitNumber *int   `json:"LogicalUnitNumber"`
	Encrypted         *bool  `json:"Encrypted"`
	Status            Status `json:"Status"`
	Links             struct {
		Drives []Link `json:"Drives"`
	} `json:"Links"`
	Operations []struct {
		OperationName      string `json:"OperationName"`
		PercentageComplete *int   `json:"PercentageComplete"`
	} `json:"Operations"`
}

// Volumes 列出全部逻辑卷
func (c *Client) Volumes(ctx context.Context, systemID, storageID string) ([]*Volume, error) {
	sysID, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+sysID+"/Storage/"+storageID+"/Volumes")
	if err != nil {
		return nil, err
	}
	out := make([]*Volume, 0, len(coll.Members))
	for _, m := range coll.Members {
		var v Volume
		if err := c.Get(ctx, m.ODataID, &v); err != nil {
			return nil, err
		}
		out = append(out, &v)
	}
	return out, nil
}

// CreateVolumeOptions 创建 RAID 卷参数（手册 3.108.4.2，全部必填项）
type CreateVolumeOptions struct {
	Name                 string
	RAIDType             string   // RAID0|RAID1|RAID5|RAID6
	StripSizeBytes       string   // 64K|128K|256K|512K|1MB
	ReadCachePolicyType  string   // Off|ReadAhead
	WriteCachePolicyType string   // WriteThrough|UnprotectedWriteBack
	ControllerURI        string   // 所属 Storage 的 @odata.id
	Initialization       string   // No_Initialization|Quick_Initialization|Full_Initialization
	DiskCachePolicy      string   // Unchanged|Enabled|Disabled
	AccessPolicy         string   // Read-Write|Read_Only|Blocked
	DeviceURIs           []string // 成员盘 Drive 的 @odata.id 列表
}

// CreateVolume 创建 RAID 卷（POST，手册实现返回 204 而非 201）。
func (c *Client) CreateVolume(ctx context.Context, systemID, storageID string, opts CreateVolumeOptions) error {
	sysID, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	body := map[string]any{
		"Name":                 opts.Name,
		"RAIDType":             opts.RAIDType,
		"StripSizeBytes":       opts.StripSizeBytes,
		"ReadCachePolicyType":  opts.ReadCachePolicyType,
		"WriteCachePolicyType": opts.WriteCachePolicyType,
		"Oem": map[string]any{
			"Ami": map[string]any{
				"ControllerID":    opts.ControllerURI,
				"Initialization":  opts.Initialization,
				"DiskCachePolicy": opts.DiskCachePolicy,
				"AccessPolicy":    opts.AccessPolicy,
				"Device":          opts.DeviceURIs,
			},
		},
	}
	_, err = c.Post(ctx, "/redfish/v1/Systems/"+sysID+"/Storage/"+storageID+"/Volumes", body, nil)
	return err
}

// ---------- SimpleStorage（手册 3.101–3.102，只读） ----------

// SimpleStorageDevice 直连设备摘要
type SimpleStorageDevice struct {
	Name          string `json:"Name"`
	Manufacturer  string `json:"Manufacturer"`
	Model         string `json:"Model"`
	CapacityBytes *int64 `json:"CapacityBytes"`
	Status        Status `json:"Status"`
}

// SimpleStorage 简单存储视图
type SimpleStorage struct {
	ODataContext   string                `json:"@odata.context"`
	ODataID        string                `json:"@odata.id"`
	ID             string                `json:"Id"`
	Name           string                `json:"Name"`
	UefiDevicePath string                `json:"UefiDevicePath"`
	Devices        []SimpleStorageDevice `json:"Devices"`
	Status         Status                `json:"Status"`
}

// SimpleStorages 列出系统简单存储视图
func (c *Client) SimpleStorages(ctx context.Context, systemID string) ([]*SimpleStorage, error) {
	sysID, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+sysID+"/SimpleStorage")
	if err != nil {
		return nil, err
	}
	out := make([]*SimpleStorage, 0, len(coll.Members))
	for _, m := range coll.Members {
		var ss SimpleStorage
		if err := c.Get(ctx, m.ODataID, &ss); err != nil {
			return nil, err
		}
		out = append(out, &ss)
	}
	return out, nil
}
