package redfish

import (
	"context"
	"fmt"
)

// ---------- MediaController（手册 3.22–3.23，只读；AST2600 实机集合为空） ----------

// MediaController 媒体控制器
type MediaController struct {
	ODataContext        string `json:"@odata.context"`
	ODataID             string `json:"@odata.id"`
	ID                  string `json:"Id"`
	Name                string `json:"Name"`
	MediaControllerType string `json:"MediaControllerType"`
	Manufacturer        string `json:"Manufacturer"`
	Model               string `json:"Model"`
	SerialNumber        string `json:"SerialNumber"`
	PartNumber          string `json:"PartNumber"`
	UUID                string `json:"UUID"`
	Status              Status `json:"Status"`
}

// MediaControllers 列出机箱媒体控制器
func (c *Client) MediaControllers(ctx context.Context, chassisID string) ([]*MediaController, error) {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Chassis/"+id+"/MediaControllers")
	if err != nil {
		return nil, err
	}
	out := make([]*MediaController, 0, len(coll.Members))
	for _, m := range coll.Members {
		var mc MediaController
		if err := c.Get(ctx, m.ODataID, &mc); err != nil {
			return nil, err
		}
		out = append(out, &mc)
	}
	return out, nil
}

// ---------- GraphicsController / USBController（手册 3.83–3.84、3.110–3.111，只读） ----------

// DeviceSummary 通用控制器摘要（Graphics/USB 同构：厂商信息 + Ports 链接）
type DeviceSummary struct {
	ODataContext    string `json:"@odata.context"`
	ODataID         string `json:"@odata.id"`
	ID              string `json:"Id"`
	Name            string `json:"Name"`
	Manufacturer    string `json:"Manufacturer"`
	Model           string `json:"Model"`
	SerialNumber    string `json:"SerialNumber"`
	PartNumber      string `json:"PartNumber"`
	SKU             string `json:"SKU"`
	SparePartNumber string `json:"SparePartNumber"`
	Ports           Link   `json:"Ports"`
	Status          Status `json:"Status"`
}

// GraphicsControllers 列出系统图形控制器
func (c *Client) GraphicsControllers(ctx context.Context, systemID string) ([]*DeviceSummary, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+id+"/GraphicsControllers")
	if err != nil {
		return nil, err
	}
	out := make([]*DeviceSummary, 0, len(coll.Members))
	for _, m := range coll.Members {
		var d DeviceSummary
		if err := c.Get(ctx, m.ODataID, &d); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, nil
}

// USBControllers 列出系统 USB 控制器
func (c *Client) USBControllers(ctx context.Context, systemID string) ([]*DeviceSummary, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+id+"/USBControllers")
	if err != nil {
		return nil, err
	}
	out := make([]*DeviceSummary, 0, len(coll.Members))
	for _, m := range coll.Members {
		var d DeviceSummary
		if err := c.Get(ctx, m.ODataID, &d); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, nil
}

// ---------- MemoryDomain / MemoryChunks（手册 3.87–3.90，只读） ----------

// MemoryDomain 内存域
type MemoryDomain struct {
	ODataContext              string `json:"@odata.context"`
	ODataID                   string `json:"@odata.id"`
	ID                        string `json:"Id"`
	Name                      string `json:"Name"`
	AllowsMemoryChunkCreation *bool  `json:"AllowsMemoryChunkCreation"`
	AllowsBlockProvisioning   *bool  `json:"AllowsBlockProvisioning"`
	AllowsMirroring           *bool  `json:"AllowsMirroring"`
	AllowsSparing             *bool  `json:"AllowsSparing"`
	MemoryChunks              Link   `json:"MemoryChunks"`
}

// MemoryDomains 列出系统内存域
func (c *Client) MemoryDomains(ctx context.Context, systemID string) ([]*MemoryDomain, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+id+"/MemoryDomains")
	if err != nil {
		return nil, err
	}
	out := make([]*MemoryDomain, 0, len(coll.Members))
	for _, m := range coll.Members {
		var md MemoryDomain
		if err := c.Get(ctx, m.ODataID, &md); err != nil {
			return nil, err
		}
		out = append(out, &md)
	}
	return out, nil
}

// MemoryChunks 内存块
type MemoryChunks struct {
	ODataContext       string `json:"@odata.context"`
	ODataID            string `json:"@odata.id"`
	ID                 string `json:"Id"`
	Name               string `json:"Name"`
	MemoryChunkSizeMiB *int   `json:"MemoryChunkSizeMiB"`
	AddressRangeType   string `json:"AddressRangeType"` // Volatile|PMEM|Block
	IsMirrorEnabled    *bool  `json:"IsMirrorEnabled"`
	IsSpare            *bool  `json:"IsSpare"`
}

// MemoryChunks 列出内存域的内存块
func (c *Client) MemoryChunks(ctx context.Context, systemID, domainID string) ([]*MemoryChunks, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, fmt.Sprintf("/redfish/v1/Systems/%s/MemoryDomains/%s/MemoryChunks", id, domainID))
	if err != nil {
		return nil, err
	}
	out := make([]*MemoryChunks, 0, len(coll.Members))
	for _, m := range coll.Members {
		var mc MemoryChunks
		if err := c.Get(ctx, m.ODataID, &mc); err != nil {
			return nil, err
		}
		out = append(out, &mc)
	}
	return out, nil
}

// ---------- HostEthernetInterfaces（手册 3.61，只读） ----------

// HostEthernetInterfaces 列出主机接口的主机侧网卡（通常需带内代理）
func (c *Client) HostEthernetInterfaces(ctx context.Context, managerID, hostIfID string) ([]string, error) {
	id, err := c.ResolveManagerID(ctx, managerID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, fmt.Sprintf("/redfish/v1/Managers/%s/HostInterfaces/%s/HostEthernetInterfaces", id, hostIfID))
	if err != nil {
		return nil, err
	}
	return coll.MemberIDs(), nil
}

// ---------- SubProcessors 处理器核/线程（手册 3.93–3.94，只读） ----------

// SubProcessors 列出处理器的子处理器（核/线程视角）
func (c *Client) SubProcessors(ctx context.Context, systemID, processorID string) ([]*Processor, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, fmt.Sprintf("/redfish/v1/Systems/%s/Processors/%s/SubProcessors", id, processorID))
	if err != nil {
		return nil, err
	}
	out := make([]*Processor, 0, len(coll.Members))
	for _, m := range coll.Members {
		var p Processor
		if err := c.Get(ctx, m.ODataID, &p); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, nil
}

// ---------- StorageControllers 独立集合（手册 3.105–3.106，只读） ----------

// StorageControllers 列出存储子系统的控制器（与 Storage.StorageControllers 内嵌摘要同源）
func (c *Client) StorageControllers(ctx context.Context, systemID, storageID string) ([]*StorageControllerInfo, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, fmt.Sprintf("/redfish/v1/Systems/%s/Storage/%s/Controllers", id, storageID))
	if err != nil {
		return nil, err
	}
	out := make([]*StorageControllerInfo, 0, len(coll.Members))
	for _, m := range coll.Members {
		var sc StorageControllerInfo
		if err := c.Get(ctx, m.ODataID, &sc); err != nil {
			return nil, err
		}
		out = append(out, &sc)
	}
	return out, nil
}

// ---------- SecureBoot 签名列表（手册 3.99–3.100） ----------

// Signatures 列出安全启动库的签名条目
func (c *Client) Signatures(ctx context.Context, systemID, databaseID string) ([]*Signature, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, fmt.Sprintf("/redfish/v1/Systems/%s/SecureBoot/SecureBootDatabases/%s/Signatures", id, databaseID))
	if err != nil {
		return nil, err
	}
	out := make([]*Signature, 0, len(coll.Members))
	for _, m := range coll.Members {
		var s Signature
		if err := c.Get(ctx, m.ODataID, &s); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, nil
}

// DeleteSignature 删除安全启动库签名条目（204）
func (c *Client) DeleteSignature(ctx context.Context, systemID, databaseID, signatureID string) error {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	return c.Delete(ctx, fmt.Sprintf("/redfish/v1/Systems/%s/SecureBoot/SecureBootDatabases/%s/Signatures/%s", id, databaseID, signatureID))
}
