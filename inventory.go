package redfish

import (
	"context"
)

// ---------- FRU 信息（手册 3.81，只读） ----------

// FRUArea FRU 分区（Board/Chassis/Product）
type FRUArea struct {
	Manufacturer string `json:"Manufacturer"`
	ProductName  string `json:"ProductName"`
	SerialNumber string `json:"SerialNumber"`
	PartNumber   string `json:"PartNumber"`
	Version      string `json:"Version"`
	AssetTag     string `json:"AssetTag"`
	SKU          string `json:"SKU"`
	Model        string `json:"Model"`
	Info         string `json:"Info"`
	ChassisType  string `json:"ChassisType"`
	UUID         string `json:"UUID"`
	Family       string `json:"Family"`
}

// FRUInfo 主板 FRU 信息
type FRUInfo struct {
	ODataContext string  `json:"@odata.context"`
	ODataID      string  `json:"@odata.id"`
	ODataType    string  `json:"@odata.type"`
	ID           string  `json:"Id"`
	Name         string  `json:"Name"`
	Board        FRUArea `json:"Board"`
	Chassis      FRUArea `json:"Chassis"`
	Product      FRUArea `json:"Product"`
}

// FRUInfo 获取系统 FRU 信息（序列号/型号/厂商的主数据源）
func (c *Client) FRUInfo(ctx context.Context, systemID string) (*FRUInfo, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	var f FRUInfo
	if err := c.Get(ctx, "/redfish/v1/Systems/"+id+"/FruInfo", &f); err != nil {
		return nil, err
	}
	return &f, nil
}

// ---------- Memory 内存（手册 3.85–3.86，只读） ----------

// Memory 内存条
type Memory struct {
	ODataContext      string `json:"@odata.context"`
	ODataID           string `json:"@odata.id"`
	ID                string `json:"Id"`
	Name              string `json:"Name"`
	CapacityMiB       *int   `json:"CapacityMiB"`
	MemoryType        string `json:"MemoryType"`       // DRAM|IntelOptane|...
	MemoryDeviceType  string `json:"MemoryDeviceType"` // DDR5|DDR4|...
	BaseModuleType    string `json:"BaseModuleType"`   // RDIMM|UDIMM|...
	Manufacturer      string `json:"Manufacturer"`
	SerialNumber      string `json:"SerialNumber"`
	PartNumber        string `json:"PartNumber"`
	OperatingSpeedMhz *int   `json:"OperatingSpeedMhz"`
	RankCount         *int   `json:"RankCount"`
	DataWidthBits     *int   `json:"DataWidthBits"`
	MemoryLocation    struct {
		Socket           *int `json:"Socket"`
		MemoryController *int `json:"MemoryController"`
		Channel          *int `json:"Channel"`
		Slot             *int `json:"Slot"`
	} `json:"MemoryLocation"`
	Metrics struct {
		ODataID string `json:"@odata.id"`
	} `json:"Metrics"` // 指向 MemoryMetrics 资源（平台未实现时缺省）
	Status Status `json:"Status"`
}

// Memories 列出系统内存条
func (c *Client) Memories(ctx context.Context, systemID string) ([]*Memory, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+id+"/Memory")
	if err != nil {
		return nil, err
	}
	out := make([]*Memory, 0, len(coll.Members))
	for _, m := range coll.Members {
		var mem Memory
		if err := c.Get(ctx, m.ODataID, &mem); err != nil {
			return nil, err
		}
		out = append(out, &mem)
	}
	return out, nil
}

// ---------- MemoryMetrics 内存健康指标（标准 schema，挂 Memory 实例下） ----------

// MemoryMetrics 单条内存的运行/寿命/纠错指标；平台未支持的字段缺省
type MemoryMetrics struct {
	ODataContext                string   `json:"@odata.context"`
	ODataID                     string   `json:"@odata.id"`
	ID                          string   `json:"Id"`
	Type                        string   `json:"Type"` // DRAM|NVDIMM_N|NVDIMM_F|NVDIMM_P|IntelOptane
	ModuleDevice                *bool    `json:"ModuleDevice"`
	DataWear                    *int     `json:"DataWear"` // 磨损百分比 0-100
	PercentEnduranceDegradation *int     `json:"PercentEnduranceDegradation"`
	BandwidthPercent            *float64 `json:"BandwidthPercent"`
	DirtyShutdownCount          *int     `json:"DirtyShutdownCount"`
	SpareBlock                  *int     `json:"SpareBlock"`
	FreeBlock                   *int     `json:"FreeBlock"`
	MemoryLatency               *int     `json:"MemoryLatency"` // 纳秒
	TemperatureCelsius          *float64 `json:"TemperatureCelsius"`
	CorrectableECCError         *int     `json:"CorrectableECCError"`
	UnCorrectableECCError       *int     `json:"UnCorrectableECCError"`
	SparesRemaining             *int     `json:"SparesRemaining"`
	RankUncorrectableErrorCount *int     `json:"RankUncorrectableErrorCount"`
	Status                      Status   `json:"Status"`
	Oem                         OemBlock `json:"Oem"`
}

// MemoryMetrics 读取单条内存的健康指标；平台未暴露该资源时返回 404 错误
func (c *Client) MemoryMetrics(ctx context.Context, systemID, memoryID string) (*MemoryMetrics, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	var m MemoryMetrics
	if err := c.Get(ctx, "/redfish/v1/Systems/"+id+"/Memory/"+memoryID+"/Metrics", &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ---------- Processor 处理器（手册 3.93–3.94，只读） ----------

// Processor 处理器
type Processor struct {
	ODataContext          string `json:"@odata.context"`
	ODataID               string `json:"@odata.id"`
	ID                    string `json:"Id"`
	Name                  string `json:"Name"`
	ProcessorType         string `json:"ProcessorType"` // CPU|GPU|FPGA|...
	Manufacturer          string `json:"Manufacturer"`
	Model                 string `json:"Model"`
	Socket                string `json:"Socket"` // P0/P1
	TotalCores            *int   `json:"TotalCores"`
	TotalEnabledCores     *int   `json:"TotalEnabledCores"`
	TotalThreads          *int   `json:"TotalThreads"`
	MaxSpeedMHz           *int   `json:"MaxSpeedMHz"`
	ProcessorArchitecture string `json:"ProcessorArchitecture"` // x86|ARM|...
	InstructionSet        string `json:"InstructionSet"`        // x86-64|...
	SerialNumber          string `json:"SerialNumber"`
	PartNumber            string `json:"PartNumber"`
	Status                Status `json:"Status"`
}

// Processors 列出系统处理器
func (c *Client) Processors(ctx context.Context, systemID string) ([]*Processor, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+id+"/Processors")
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

// ---------- NetworkInterface 主机侧网卡（手册 3.91–3.92，只读） ----------

// NetworkInterface 主机侧网络接口
type NetworkInterface struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	Status       Status `json:"Status"`
	Links        struct {
		NetworkAdapter Link `json:"NetworkAdapter"`
	} `json:"Links"`
	NetworkDeviceFunctions Link `json:"NetworkDeviceFunctions"`
}

// NetworkInterfaces 列出主机侧网络接口
func (c *Client) NetworkInterfaces(ctx context.Context, systemID string) ([]*NetworkInterface, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+id+"/NetworkInterfaces")
	if err != nil {
		return nil, err
	}
	out := make([]*NetworkInterface, 0, len(coll.Members))
	for _, m := range coll.Members {
		var ni NetworkInterface
		if err := c.Get(ctx, m.ODataID, &ni); err != nil {
			return nil, err
		}
		out = append(out, &ni)
	}
	return out, nil
}

// ---------- NetworkAdapter 网络适配器（手册 3.24–3.25，只读） ----------

// NetworkAdapter 网络适配器（含控制器 ASIC 信息）
type NetworkAdapter struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	Manufacturer string `json:"Manufacturer"`
	Model        string `json:"Model"`
	SerialNumber string `json:"SerialNumber"`
	PartNumber   string `json:"PartNumber"`
	Controllers  []struct {
		FirmwarePackageVersion string `json:"FirmwarePackageVersion"`
		ControllerCapabilities struct {
			NetworkPortCount           *int `json:"NetworkPortCount"`
			NetworkDeviceFunctionCount *int `json:"NetworkDeviceFunctionCount"`
		} `json:"ControllerCapabilities"`
	} `json:"Controllers"`
	Status Status `json:"Status"`
}

// NetworkAdapters 列出机箱网络适配器
func (c *Client) NetworkAdapters(ctx context.Context, chassisID string) ([]*NetworkAdapter, error) {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Chassis/"+id+"/NetworkAdapters")
	if err != nil {
		return nil, err
	}
	out := make([]*NetworkAdapter, 0, len(coll.Members))
	for _, m := range coll.Members {
		var na NetworkAdapter
		if err := c.Get(ctx, m.ODataID, &na); err != nil {
			return nil, err
		}
		out = append(out, &na)
	}
	return out, nil
}

// ---------- PCIe 设备（手册 3.34–3.38，只读，自动翻页） ----------

// PCIeDevice PCIe 设备
type PCIeDevice struct {
	ODataContext    string `json:"@odata.context"`
	ODataID         string `json:"@odata.id"`
	ID              string `json:"Id"` // BDF 十六进制：00_01_00
	Name            string `json:"Name"`
	Manufacturer    string `json:"Manufacturer"`
	Model           string `json:"Model"`
	SerialNumber    string `json:"SerialNumber"`
	DeviceType      string `json:"DeviceType"` // SingleFunction|MultiFunction|Simulated
	FirmwareVersion string `json:"FirmwareVersion"`
	PCIeInterface   struct {
		LanesInUse  *int   `json:"LanesInUse"`
		MaxLanes    *int   `json:"MaxLanes"`
		PCIeType    string `json:"PCIeType"` // 在用代际 Gen1..Gen5
		MaxPCIeType string `json:"MaxPCIeType"`
	} `json:"PCIeInterface"`
	Status Status `json:"Status"`
}

// PCIeDevices 列出机箱全部 PCIe 设备（大集合自动翻页）
func (c *Client) PCIeDevices(ctx context.Context, chassisID string) ([]*PCIeDevice, error) {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Chassis/"+id+"/PCIeDevices")
	if err != nil {
		return nil, err
	}
	out := make([]*PCIeDevice, 0, len(coll.Members))
	for _, m := range coll.Members {
		var d PCIeDevice
		if err := c.Get(ctx, m.ODataID, &d); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, nil
}

// PCIeSlotInfo PCIe 插槽
type PCIeSlotInfo struct {
	Lanes    *int   `json:"Lanes"`
	PCIeType string `json:"PCIeType"`
	SlotType string `json:"SlotType"` // FullLength|HalfLength|LowProfile|M2|Mini
	Location struct {
		PartLocation struct {
			LocationOrdinalValue *int   `json:"LocationOrdinalValue"`
			LocationType         string `json:"LocationType"`
			ServiceLabel         string `json:"ServiceLabel"` // PCIE3/MCIO3-1/M2_2...
		} `json:"PartLocation"`
	} `json:"Location"`
	Status Status `json:"Status"`
}

// PCIeSlots 获取机箱 PCIe 插槽布局（空槽 Status.State=Absent）
func (c *Client) PCIeSlots(ctx context.Context, chassisID string) ([]PCIeSlotInfo, error) {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Slots []PCIeSlotInfo `json:"Slots"`
	}
	if err := c.Get(ctx, "/redfish/v1/Chassis/"+id+"/PCIeSlots", &resp); err != nil {
		return nil, err
	}
	return resp.Slots, nil
}

// ---------- BootOption 启动项（手册 3.79–3.80） ----------

// BootOption 启动项实例
type BootOption struct {
	ODataContext        string `json:"@odata.context"`
	ODataID             string `json:"@odata.id"`
	ODataType           string `json:"@odata.type"`
	ID                  string `json:"Id"`
	Name                string `json:"Name"`
	DisplayName         string `json:"DisplayName"`
	BootOptionReference string `json:"BootOptionReference"` // Boot0000...
	BootOptionEnabled   *bool  `json:"BootOptionEnabled"`
	Alias               string `json:"Alias"` // Hdd|Pxe|Cd|...
	UefiDevicePath      string `json:"UefiDevicePath"`
}

// BootOptions 列出系统启动项
func (c *Client) BootOptions(ctx context.Context, systemID string) ([]*BootOption, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+id+"/BootOptions")
	if err != nil {
		return nil, err
	}
	out := make([]*BootOption, 0, len(coll.Members))
	for _, m := range coll.Members {
		var bo BootOption
		if err := c.Get(ctx, m.ODataID, &bo); err != nil {
			return nil, err
		}
		out = append(out, &bo)
	}
	return out, nil
}

// CreateBootOption 创建带外启动项（201，手册 3.79.4.2）
func (c *Client) CreateBootOption(ctx context.Context, systemID, name, displayName, bootOptionRef, uefiDevicePath, alias string) (*BootOption, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"Name":                name,
		"Description":         "UEFI Boot Option",
		"BootOptionReference": bootOptionRef,
		"DisplayName":         displayName,
		"UefiDevicePath":      uefiDevicePath,
		"Alias":               alias,
	}
	var bo BootOption
	if _, err := c.Post(ctx, "/redfish/v1/Systems/"+id+"/BootOptions", body, &bo); err != nil {
		return nil, err
	}
	return &bo, nil
}
