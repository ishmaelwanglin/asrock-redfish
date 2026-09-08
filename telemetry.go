package redfish

import (
	"context"
)

// ---------- TelemetryService 遥测（手册 3.115–3.123） ----------

// 聚合函数（手册 3.115 SupportedCollectionFunctions）
const (
	CollectionAverage   = "Average"
	CollectionMaximum   = "Maximum"
	CollectionMinimum   = "Minimum"
	CollectionSummation = "Summation"
)

// TelemetryService 遥测服务
type TelemetryService struct {
	ODataContext                 string   `json:"@odata.context"`
	ODataID                      string   `json:"@odata.id"`
	ODataType                    string   `json:"@odata.type"`
	ID                           string   `json:"Id"`
	Name                         string   `json:"Name"`
	ServiceEnabled               *bool    `json:"ServiceEnabled"`
	MaxReports                   *int     `json:"MaxReports"`            // 上限 5
	MinCollectionInterval        string   `json:"MinCollectionInterval"` // 下限 PT10M
	SupportedCollectionFunctions []string `json:"SupportedCollectionFunctions"`
	MetricDefinitions            Link     `json:"MetricDefinitions"`
	MetricReportDefinitions      Link     `json:"MetricReportDefinitions"`
	MetricReports                Link     `json:"MetricReports"`
	Triggers                     Link     `json:"Triggers"`
	Status                       Status   `json:"Status"`
}

// TelemetryService 获取遥测服务
func (c *Client) TelemetryService(ctx context.Context) (*TelemetryService, error) {
	var ts TelemetryService
	if err := c.Get(ctx, "/redfish/v1/TelemetryService", &ts); err != nil {
		return nil, err
	}
	return &ts, nil
}

// SetTelemetryEnabled 开关遥测服务（204）
func (c *Client) SetTelemetryEnabled(ctx context.Context, enabled bool) error {
	return c.Patch(ctx, "/redfish/v1/TelemetryService", map[string]bool{"ServiceEnabled": enabled}, nil)
}

// ---------- MetricReportDefinition 周期报表定义（手册 3.118–3.119） ----------

// ReportMetric Metrics 数组元素（带聚合函数的指标）
type ReportMetric struct {
	MetricID            string   `json:"MetricId,omitempty"`
	CollectionFunction  string   `json:"CollectionFunction,omitempty"`  // Average|Maximum|Minimum|Summation
	CollectionDuration  string   `json:"CollectionDuration,omitempty"`  // 如 PT10M（>=PT10M）
	CollectionTimeScope string   `json:"CollectionTimeScope,omitempty"` // 仅 Interval
	MetricProperties    []string `json:"MetricProperties,omitempty"`    // RFC6901 URI
}

// MetricReportDefinition 报表定义
type MetricReportDefinition struct {
	ODataContext                  string   `json:"@odata.context"`
	ODataID                       string   `json:"@odata.id"`
	ODataType                     string   `json:"@odata.type"`
	ID                            string   `json:"Id"`
	Name                          string   `json:"Name"`
	MetricReportDefinitionType    string   `json:"MetricReportDefinitionType"` // 仅 Periodic
	MetricReportDefinitionEnabled *bool    `json:"MetricReportDefinitionEnabled"`
	SuppressRepeatedMetricValue   *bool    `json:"SuppressRepeatedMetricValue"`
	MetricReportHeartbeatInterval *string  `json:"MetricReportHeartbeatInterval"`
	AppendLimit                   *int     `json:"AppendLimit"`
	ReportActions                 []string `json:"ReportActions"` // LogToMetricReportsCollection|RedfishEvent
	ReportUpdates                 string   `json:"ReportUpdates"` // Overwrite|AppendWrapsWhenFull|...
	Schedule                      struct {
		RecurrenceInterval string `json:"RecurrenceInterval"` // 仅 PT10M
	} `json:"Schedule"`
	MetricReport     Link           `json:"MetricReport"`
	MetricProperties []string       `json:"MetricProperties"`
	Metrics          []ReportMetric `json:"Metrics"`
}

// CreateReportDefinitionOptions 创建报表定义参数（必填：ID/Name/属性或指标列表）
type CreateReportDefinitionOptions struct {
	ID               string
	Name             string
	MetricProperties []string       // 直接采集的属性 URI（与 Metrics 二选一）
	Metrics          []ReportMetric // 带聚合函数的指标（与 MetricProperties 二选一）
}

// CreateReportDefinition 创建周期报表（201）。采集周期固定 PT10M（固件限制）。
func (c *Client) CreateReportDefinition(ctx context.Context, opts CreateReportDefinitionOptions) (*MetricReportDefinition, error) {
	body := map[string]any{
		"Id":                         opts.ID,
		"Name":                       opts.Name,
		"Schedule":                   map[string]string{"RecurrenceInterval": "PT10M"},
		"MetricReportDefinitionType": "Periodic",
		"MetricReport":               map[string]string{"@odata.id": "/redfish/v1/TelemetryService/MetricReports/" + opts.ID},
	}
	if opts.MetricProperties != nil {
		body["MetricProperties"] = opts.MetricProperties
	}
	if opts.Metrics != nil {
		body["Metrics"] = opts.Metrics
	}
	var mrd MetricReportDefinition
	if _, err := c.Post(ctx, "/redfish/v1/TelemetryService/MetricReportDefinitions", body, &mrd); err != nil {
		return nil, err
	}
	return &mrd, nil
}

// ReportDefinitions 列出全部报表定义
func (c *Client) ReportDefinitions(ctx context.Context) ([]*MetricReportDefinition, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/TelemetryService/MetricReportDefinitions")
	if err != nil {
		return nil, err
	}
	out := make([]*MetricReportDefinition, 0, len(coll.Members))
	for _, m := range coll.Members {
		var mrd MetricReportDefinition
		if err := c.Get(ctx, m.ODataID, &mrd); err != nil {
			return nil, err
		}
		out = append(out, &mrd)
	}
	return out, nil
}

// UpdateReportDefinition 更新报表定义（启停/抑制重复值/心跳间隔，204）
func (c *Client) UpdateReportDefinition(ctx context.Context, defID string, enabled, suppressRepeated *bool, heartbeatInterval string) error {
	body := map[string]any{}
	if enabled != nil {
		body["MetricReportDefinitionEnabled"] = *enabled
	}
	if suppressRepeated != nil {
		body["SuppressRepeatedMetricValue"] = *suppressRepeated
	}
	if heartbeatInterval != "" {
		body["MetricReportHeartbeatInterval"] = heartbeatInterval
	}
	return c.Patch(ctx, "/redfish/v1/TelemetryService/MetricReportDefinitions/"+defID, body, nil)
}

// DeleteReportDefinition 删除报表定义（204）
func (c *Client) DeleteReportDefinition(ctx context.Context, defID string) error {
	return c.Delete(ctx, "/redfish/v1/TelemetryService/MetricReportDefinitions/"+defID)
}

// ---------- MetricReport 报表（手册 3.120–3.121，只读） ----------

// MetricValue 单条指标值
type MetricValue struct {
	MetricID       string `json:"MetricId"`
	MetricValue    string `json:"MetricValue"`
	Timestamp      string `json:"TimeStamp"`
	MetricProperty string `json:"MetricProperty"`
}

// MetricReport 报表实例
type MetricReport struct {
	ODataContext string        `json:"@odata.context"`
	ODataID      string        `json:"@odata.id"`
	ODataType    string        `json:"@odata.type"`
	ID           string        `json:"Id"`
	Name         string        `json:"Name"`
	Timestamp    string        `json:"Timestamp"`
	MetricValues []MetricValue `json:"MetricValues"`
}

// MetricReports 列出全部报表
func (c *Client) MetricReports(ctx context.Context) ([]*MetricReport, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/TelemetryService/MetricReports")
	if err != nil {
		return nil, err
	}
	out := make([]*MetricReport, 0, len(coll.Members))
	for _, m := range coll.Members {
		var mr MetricReport
		if err := c.Get(ctx, m.ODataID, &mr); err != nil {
			return nil, err
		}
		out = append(out, &mr)
	}
	return out, nil
}

// ---------- Trigger 阈值触发器（手册 3.122–3.123） ----------

// 触发动作
const (
	TriggerActionLogToLogService     = "LogToLogService"
	TriggerActionRedfishEvent        = "RedfishEvent"
	TriggerActionRedfishMetricReport = "RedfishMetricReport"
)

// Threshold 阈值定义
type Threshold struct {
	Activation string  `json:"Activation"` // Increasing|Decreasing|Either
	DwellTime  string  `json:"DwellTime"`  // 如 PT1M
	Reading    float64 `json:"Reading"`
}

// NumericThresholds 四级阈值
type NumericThresholds struct {
	LowerCritical *Threshold `json:"LowerCritical,omitempty"`
	LowerWarning  *Threshold `json:"LowerWarning,omitempty"`
	UpperCritical *Threshold `json:"UpperCritical,omitempty"`
	UpperWarning  *Threshold `json:"UpperWarning,omitempty"`
}

// DiscreteTrigger 离散触发条件
type DiscreteTrigger struct {
	Name      string `json:"Name"`
	Value     string `json:"Value"`
	Severity  string `json:"Severity"` // OK|Warning|Critical
	DwellTime string `json:"DwellTime"`
}

// Trigger 触发器实例
type Trigger struct {
	ODataContext             string            `json:"@odata.context"`
	ODataID                  string            `json:"@odata.id"`
	ODataType                string            `json:"@odata.type"`
	ID                       string            `json:"Id"`
	Name                     string            `json:"Name"`
	MetricType               string            `json:"MetricType"` // Numeric|Discrete
	TriggerActions           []string          `json:"TriggerActions"`
	DiscreteTriggerCondition string            `json:"DiscreteTriggerCondition"` // Changed|Specified
	DiscreteTriggers         []DiscreteTrigger `json:"DiscreteTriggers"`
	NumericThresholds        NumericThresholds `json:"NumericThresholds"`
	MetricProperties         []string          `json:"MetricProperties"`
}

// CreateNumericTrigger 创建数值阈值触发器（201）。
// 例：CPU 温度 >80°C 触发事件——MetricProperties 指向 Thermal#/Temperatures/n/ReadingCelsius。
func (c *Client) CreateNumericTrigger(ctx context.Context, id, name string, actions []string, thresholds NumericThresholds, metricProperties []string) (*Trigger, error) {
	body := map[string]any{
		"Id":                id,
		"Name":              name,
		"MetricType":        "Numeric",
		"TriggerActions":    actions,
		"NumericThresholds": thresholds,
		"MetricProperties":  metricProperties,
	}
	var t Trigger
	if _, err := c.Post(ctx, "/redfish/v1/TelemetryService/Triggers", body, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// CreateDiscreteTrigger 创建离散值触发器（值变化/命中指定值时触发，201）
func (c *Client) CreateDiscreteTrigger(ctx context.Context, id, name string, actions []string, condition string, triggers []DiscreteTrigger, metricProperties []string) (*Trigger, error) {
	body := map[string]any{
		"Id":                       id,
		"Name":                     name,
		"MetricType":               "Discrete",
		"TriggerActions":           actions,
		"DiscreteTriggerCondition": condition,
		"DiscreteTriggers":         triggers,
		"MetricProperties":         metricProperties,
	}
	var t Trigger
	if _, err := c.Post(ctx, "/redfish/v1/TelemetryService/Triggers", body, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Triggers 列出全部触发器
func (c *Client) Triggers(ctx context.Context) ([]*Trigger, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/TelemetryService/Triggers")
	if err != nil {
		return nil, err
	}
	out := make([]*Trigger, 0, len(coll.Members))
	for _, m := range coll.Members {
		var t Trigger
		if err := c.Get(ctx, m.ODataID, &t); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, nil
}

// DeleteTrigger 删除触发器（204）
func (c *Client) DeleteTrigger(ctx context.Context, triggerID string) error {
	return c.Delete(ctx, "/redfish/v1/TelemetryService/Triggers/"+triggerID)
}

// ---------- MetricDefinition 指标元数据（手册 3.116–3.117，只读） ----------

// MetricDefinition 指标定义
type MetricDefinition struct {
	ODataContext     string   `json:"@odata.context"`
	ODataID          string   `json:"@odata.id"`
	ID               string   `json:"Id"`
	Name             string   `json:"Name"`
	MetricDataType   string   `json:"MetricDataType"`
	MetricType       string   `json:"MetricType"`
	Implementation   string   `json:"Implementation"` // PhysicalSensor|Calculated|...
	Units            string   `json:"Units"`
	IsLinear         *bool    `json:"IsLinear"`
	Precision        *int     `json:"Precision"`
	Accuracy         *float64 `json:"Accuracy"`
	MetricProperties []string `json:"MetricProperties"`
}

// MetricDefinitions 列出全部指标定义
func (c *Client) MetricDefinitions(ctx context.Context) ([]*MetricDefinition, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/TelemetryService/MetricDefinitions")
	if err != nil {
		return nil, err
	}
	out := make([]*MetricDefinition, 0, len(coll.Members))
	for _, m := range coll.Members {
		var md MetricDefinition
		if err := c.Get(ctx, m.ODataID, &md); err != nil {
			return nil, err
		}
		out = append(out, &md)
	}
	return out, nil
}

// SubmitTestMetricReport 提交测试报表（202 → 任务 URI）
func (c *Client) SubmitTestMetricReport(ctx context.Context, reportName string, values []MetricValue) (string, error) {
	body := map[string]any{
		"MetricReportName":            reportName,
		"GeneratedMetricReportValues": values,
	}
	return c.runAction(ctx, "/redfish/v1/TelemetryService/Actions/TelemetryService.SubmitTestMetricReport", body)
}
