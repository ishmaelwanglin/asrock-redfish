package redfish

import (
	"context"
	"encoding/json"
)

// ---------- Chassis（手册 3.16–3.17） ----------

// Chassis 机箱资源
type Chassis struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ODataType    string `json:"@odata.type"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	Description  string `json:"Description"`

	ChassisType  string `json:"ChassisType"` // RackMount|Rack|Blade|Enclosure|...
	PowerState   string `json:"PowerState"`  // On|Off
	AssetTag     string `json:"AssetTag"`
	IndicatorLED string `json:"IndicatorLED"`
	Manufacturer string `json:"Manufacturer"`
	Model        string `json:"Model"`
	SKU          string `json:"SKU"`
	SerialNumber string `json:"SerialNumber"`
	PartNumber   string `json:"PartNumber"`
	UUID         string `json:"UUID"`
	Status       Status `json:"Status"`

	Thermal          Link `json:"Thermal"`
	Power            Link `json:"Power"`
	Sensors          Link `json:"Sensors"`
	LogServices      Link `json:"LogServices"`
	NetworkAdapters  Link `json:"NetworkAdapters"`
	MediaControllers Link `json:"MediaControllers"`
	PCIeDevices      Link `json:"PCIeDevices"`
	PCIeSlots        Link `json:"PCIeSlots"`
}

// ChassisList 列出机箱成员 URI
func (c *Client) ChassisList(ctx context.Context) ([]string, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Chassis")
	if err != nil {
		return nil, err
	}
	return coll.MemberIDs(), nil
}

// Chassis 获取机箱资源。chassisID 空 → 自动发现。
func (c *Client) Chassis(ctx context.Context, chassisID string) (*Chassis, error) {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return nil, err
	}
	var ch Chassis
	if err := c.Get(ctx, "/redfish/v1/Chassis/"+id, &ch); err != nil {
		return nil, err
	}
	return &ch, nil
}

// UpdateChassis 更新机箱可写字段（AssetTag/IndicatorLED，204）
func (c *Client) UpdateChassis(ctx context.Context, chassisID, assetTag, indicatorLED string) error {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if assetTag != "" {
		body["AssetTag"] = assetTag
	}
	if indicatorLED != "" {
		body["IndicatorLED"] = indicatorLED
	}
	return c.Patch(ctx, "/redfish/v1/Chassis/"+id, body, nil)
}

// ChassisReset 机箱级电源操作（202 → 任务 URI；等效系统电源控制）
func (c *Client) ChassisReset(ctx context.Context, chassisID, resetType string) (string, error) {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return "", err
	}
	return c.runAction(ctx, "/redfish/v1/Chassis/"+id+"/Actions/Chassis.Reset",
		map[string]string{"ResetType": resetType})
}

// ---------- Power 电源（手册 3.39） ----------

// PowerLimit 功率限额
type PowerLimit struct {
	LimitInWatts   *int    `json:"LimitInWatts"`
	LimitException *string `json:"LimitException"` // NoAction|HardPowerOff|LogEventOnly|Oem
	CorrectionInMs *int    `json:"CorrectionInMs"`
}

// PowerControl 功率控制组
type PowerControl struct {
	PowerConsumedWatts  *float64     `json:"PowerConsumedWatts"`
	PowerAllocatedWatts *float64     `json:"PowerAllocatedWatts"`
	PowerCapacityWatts  *float64     `json:"PowerCapacityWatts"`
	PowerLimit          PowerLimit   `json:"PowerLimit"`
	PowerMetrics        PowerMetrics `json:"PowerMetrics"`
}

// PowerMetrics 功率统计窗口
type PowerMetrics struct {
	AverageConsumedWatts *float64 `json:"AverageConsumedWatts"`
	MinConsumedWatts     *float64 `json:"MinConsumedWatts"`
	MaxConsumedWatts     *float64 `json:"MaxConsumedWatts"`
	IntervalInMin        *int     `json:"IntervalInMin"`
}

// Voltage 电压传感器
type Voltage struct {
	MemberID               string   `json:"MemberId"`
	Name                   string   `json:"Name"`
	ReadingVolts           *float64 `json:"ReadingVolts"`
	SensorNumber           *int     `json:"SensorNumber"`
	UpperThresholdCritical *float64 `json:"UpperThresholdCritical"`
	UpperThresholdFatal    *float64 `json:"UpperThresholdFatal"`
	LowerThresholdCritical *float64 `json:"LowerThresholdCritical"`
	LowerThresholdFatal    *float64 `json:"LowerThresholdFatal"`
	Status                 Status   `json:"Status"`
}

// PowerSupply 电源模块
type PowerSupply struct {
	MemberID             string   `json:"MemberId"`
	Name                 string   `json:"Name"`
	PowerSupplyType      string   `json:"PowerSupplyType"` // AC|DC|ACorDC
	PowerCapacityWatts   *float64 `json:"PowerCapacityWatts"`
	LastPowerOutputWatts *float64 `json:"LastPowerOutputWatts"`
	InputRanges          []struct {
		InputType      string   `json:"InputType"`
		MinimumVoltage *float64 `json:"MinimumVoltage"`
		MaximumVoltage *float64 `json:"MaximumVoltage"`
		OutputWattage  *float64 `json:"OutputWattage"`
	} `json:"InputRanges"`
	Model           string `json:"Model"`
	Manufacturer    string `json:"Manufacturer"`
	SerialNumber    string `json:"SerialNumber"`
	PartNumber      string `json:"PartNumber"`
	FirmwareVersion string `json:"FirmwareVersion"`
	HotPluggable    *bool  `json:"HotPluggable"`
	Status          Status `json:"Status"`
}

// Power 电源资源（手册 3.39）
type Power struct {
	ODataContext  string          `json:"@odata.context"`
	ODataID       string          `json:"@odata.id"`
	ODataType     string          `json:"@odata.type"`
	ID            string          `json:"Id"`
	Name          string          `json:"Name"`
	PowerControl  []PowerControl  `json:"PowerControl"`
	Voltages      []Voltage       `json:"Voltages"`
	PowerSupplies []PowerSupply   `json:"PowerSupplies"`
	Oem           json.RawMessage `json:"Oem"`
}

// Power 获取机箱电源信息。chassisID 空 → 自动发现。
func (c *Client) Power(ctx context.Context, chassisID string) (*Power, error) {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return nil, err
	}
	var p Power
	if err := c.Get(ctx, "/redfish/v1/Chassis/"+id+"/Power", &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// SetPowerLimit 设置功率限额（PATCH PowerControl[0].PowerLimit，200 返回更新后资源）
func (c *Client) SetPowerLimit(ctx context.Context, chassisID string, limit PowerLimit) error {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return err
	}
	body := map[string]any{
		"PowerControl": []map[string]any{{"PowerLimit": limit}},
	}
	return c.Patch(ctx, "/redfish/v1/Chassis/"+id+"/Power", body, nil)
}

// ConsumedWatts 当前整机功耗（瓦）；PowerControl 为空时返回 0
func (p *Power) ConsumedWatts() float64 {
	if len(p.PowerControl) > 0 && p.PowerControl[0].PowerConsumedWatts != nil {
		return *p.PowerControl[0].PowerConsumedWatts
	}
	return 0
}

// ---------- Thermal 散热（手册 3.42） ----------

// Temperature 温度传感器
type Temperature struct {
	MemberID                  string   `json:"MemberId"`
	Name                      string   `json:"Name"`
	ReadingCelsius            *float64 `json:"ReadingCelsius"`
	SensorNumber              *int     `json:"SensorNumber"`
	UpperThresholdNonCritical *float64 `json:"UpperThresholdNonCritical"`
	UpperThresholdCritical    *float64 `json:"UpperThresholdCritical"`
	UpperThresholdFatal       *float64 `json:"UpperThresholdFatal"`
	PhysicalContext           string   `json:"PhysicalContext"` // CPU|GPU|Intake|Exhaust|...
	Status                    Status   `json:"Status"`
}

// Fan 风扇
type Fan struct {
	MemberID               string   `json:"MemberId"`
	Name                   string   `json:"Name"`
	Reading                *float64 `json:"Reading"`
	ReadingUnits           string   `json:"ReadingUnits"` // RPM|%|...
	SensorNumber           *int     `json:"SensorNumber"`
	UpperThresholdCritical *float64 `json:"UpperThresholdCritical"`
	LowerThresholdCritical *float64 `json:"LowerThresholdCritical"`
	HotPluggable           *bool    `json:"HotPluggable"`
	Status                 Status   `json:"Status"`
}

// Thermal 散热资源
type Thermal struct {
	ODataContext string          `json:"@odata.context"`
	ODataID      string          `json:"@odata.id"`
	ODataType    string          `json:"@odata.type"`
	ID           string          `json:"Id"`
	Name         string          `json:"Name"`
	Temperatures []Temperature   `json:"Temperatures"`
	Fans         []Fan           `json:"Fans"`
	Oem          json.RawMessage `json:"Oem"`
}

// Thermal 获取机箱散热信息（温度/风扇）
func (c *Client) Thermal(ctx context.Context, chassisID string) (*Thermal, error) {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return nil, err
	}
	var t Thermal
	if err := c.Get(ctx, "/redfish/v1/Chassis/"+id+"/Thermal", &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// ---------- Sensor 传感器（手册 3.40–3.41） ----------

// SensorThreshold 阈值定义
type SensorThreshold struct {
	Activation string   `json:"Activation"` // Increasing|Decreasing|Either
	DwellTime  string   `json:"DwellTime"`
	Reading    *float64 `json:"Reading"`
}

// Sensor 传感器实例
type Sensor struct {
	ODataContext    string                     `json:"@odata.context"`
	ODataID         string                     `json:"@odata.id"`
	ODataType       string                     `json:"@odata.type"`
	ID              string                     `json:"Id"`
	Name            string                     `json:"Name"`
	Reading         *float64                   `json:"Reading"`
	ReadingType     string                     `json:"ReadingType"` // Temperature|Voltage|Current|Fan|Power|...
	ReadingUnits    string                     `json:"ReadingUnits"`
	PhysicalContext string                     `json:"PhysicalContext"`
	Thresholds      map[string]SensorThreshold `json:"Thresholds"` // 键: LowerCaution/LowerCritical/UpperCaution/UpperCritical/...
	Status          Status                     `json:"Status"`
}

// Sensors 列出机箱全部传感器实例
func (c *Client) Sensors(ctx context.Context, chassisID string) ([]*Sensor, error) {
	id, err := c.ResolveChassisID(ctx, chassisID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Chassis/"+id+"/Sensors")
	if err != nil {
		return nil, err
	}
	out := make([]*Sensor, 0, len(coll.Members))
	for _, m := range coll.Members {
		var s Sensor
		if err := c.Get(ctx, m.ODataID, &s); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, nil
}
