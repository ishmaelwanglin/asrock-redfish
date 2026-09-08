package redfish

import (
	"context"
	"fmt"
)

// ---------- EventService 事件服务（手册 3.48–3.51） ----------

// SMTPSettings SMTP 告警投递设置
type SMTPSettings struct {
	ServiceEnabled     *bool   `json:"ServiceEnabled,omitempty"`
	ServerAddress      string  `json:"ServerAddress,omitempty"`
	Port               *int    `json:"Port,omitempty"`
	FromAddress        string  `json:"FromAddress,omitempty"`
	Username           string  `json:"Username,omitempty"`
	Password           string  `json:"Password,omitempty"`
	Authentication     *string `json:"Authentication,omitempty"`     // Plain|None
	ConnectionProtocol *string `json:"ConnectionProtocol,omitempty"` // None|StartTLS|TLS_SSL
}

// EventService 事件服务
type EventService struct {
	ODataContext                 string       `json:"@odata.context"`
	ODataID                      string       `json:"@odata.id"`
	ODataType                    string       `json:"@odata.type"`
	ID                           string       `json:"Id"`
	Name                         string       `json:"Name"`
	ServiceEnabled               *bool        `json:"ServiceEnabled"`
	DeliveryRetryAttempts        *int         `json:"DeliveryRetryAttempts"`
	DeliveryRetryIntervalSeconds *int         `json:"DeliveryRetryIntervalSeconds"`
	EventFormatTypes             []string     `json:"EventFormatTypes"` // MetricReport|Event
	RegistryPrefixes             []string     `json:"RegistryPrefixes"`
	ResourceTypes                []string     `json:"ResourceTypes"`
	ServerSentEventUri           string       `json:"ServerSentEventUri"` // SSE 流端点
	Subscriptions                Link         `json:"Subscriptions"`
	SMTP                         SMTPSettings `json:"SMTP"`
	Status                       Status       `json:"Status"`
}

// EventService 获取事件服务（订阅前的能力探测：RegistryPrefixes/ResourceTypes 白名单）
func (c *Client) EventService(ctx context.Context) (*EventService, error) {
	var es EventService
	if err := c.Get(ctx, "/redfish/v1/EventService", &es); err != nil {
		return nil, err
	}
	return &es, nil
}

// UpdateEventService 更新事件服务设置（ServiceEnabled/重试策略/SMTP，204）
func (c *Client) UpdateEventService(ctx context.Context, body map[string]any) error {
	return c.Patch(ctx, "/redfish/v1/EventService", body, nil)
}

// ConfigureSMTP 配置 SMTP 告警投递（204）
func (c *Client) ConfigureSMTP(ctx context.Context, smtp SMTPSettings) error {
	return c.UpdateEventService(ctx, map[string]any{"SMTP": smtp})
}

// SubmitTestEvent 发送测试事件（验证订阅链路，202 → 任务 URI）
func (c *Client) SubmitTestEvent(ctx context.Context, messageID string) (string, error) {
	return c.runAction(ctx, "/redfish/v1/EventService/Actions/EventService.SubmitTestEvent",
		map[string]string{"MessageId": messageID})
}

// ---------- EventSubscription 事件订阅（手册 3.49–3.50） ----------

// Subscription 订阅实例（EventDestination）
type Subscription struct {
	ODataContext            string   `json:"@odata.context"`
	ODataID                 string   `json:"@odata.id"`
	ODataType               string   `json:"@odata.type"`
	ID                      string   `json:"Id"`
	Name                    string   `json:"Name"`
	Destination             string   `json:"Destination"`      // 回调 URL 或 SMTP 时的邮件地址
	Protocol                string   `json:"Protocol"`         // Redfish|SMTP
	EventFormatType         string   `json:"EventFormatType"`  // Event|MetricReport
	SubscriptionType        string   `json:"SubscriptionType"` // RedfishEvent|SSE|SNMPTrap|...
	Context                 string   `json:"Context"`
	RegistryPrefixes        []string `json:"RegistryPrefixes"`
	ResourceTypes           []string `json:"ResourceTypes"`
	MessageIds              []string `json:"MessageIds"`
	OriginResources         []Link   `json:"OriginResources"`
	SubordinateResources    *bool    `json:"SubordinateResources"`
	MetricReportDefinitions []Link   `json:"MetricReportDefinitions"`
	DeliveryRetryPolicy     *string  `json:"DeliveryRetryPolicy"` // RetryForever|SuspendRetries|TerminateAfterRetries
	Status                  Status   `json:"Status"`
}

// SubscribeOptions 创建订阅参数
type SubscribeOptions struct {
	Destination      string   // 事件回调 URL（http://host:port/event）
	Context          string   // 自定义上下文，随事件原样回传
	EventFormatType  string   // Event|MetricReport（默认 Event）
	RegistryPrefixes []string // 白名单（nil=全部）
	ResourceTypes    []string // 白名单（nil=全部）
	OriginResources  []string // 仅订阅指定资源（@odata.id 列表；nil=全部）
}

// Subscribe 创建事件订阅（201 返回订阅实例）
func (c *Client) Subscribe(ctx context.Context, opts SubscribeOptions) (*Subscription, error) {
	format := opts.EventFormatType
	if format == "" {
		format = "Event"
	}
	body := map[string]any{
		"Destination":     opts.Destination,
		"Protocol":        "Redfish",
		"EventFormatType": format,
	}
	if opts.Context != "" {
		body["Context"] = opts.Context
	}
	if opts.RegistryPrefixes != nil {
		body["RegistryPrefixes"] = opts.RegistryPrefixes
	}
	if opts.ResourceTypes != nil {
		body["ResourceTypes"] = opts.ResourceTypes
	}
	if opts.OriginResources != nil {
		origins := make([]Link, 0, len(opts.OriginResources))
		for _, u := range opts.OriginResources {
			origins = append(origins, Link{ODataID: u})
		}
		body["OriginResources"] = origins
	}
	var s Subscription
	if _, err := c.Post(ctx, "/redfish/v1/EventService/Subscriptions", body, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Subscriptions 列出全部订阅
func (c *Client) Subscriptions(ctx context.Context) ([]*Subscription, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/EventService/Subscriptions")
	if err != nil {
		return nil, err
	}
	out := make([]*Subscription, 0, len(coll.Members))
	for _, m := range coll.Members {
		var s Subscription
		if err := c.Get(ctx, m.ODataID, &s); err != nil {
			return nil, err
		}
		out = append(out, &s)
	}
	return out, nil
}

// UpdateSubscription 更新订阅（Context/DeliveryRetryPolicy，204）
func (c *Client) UpdateSubscription(ctx context.Context, subID string, body map[string]any) error {
	return c.Patch(ctx, "/redfish/v1/EventService/Subscriptions/"+subID, body, nil)
}

// ResumeSubscription 恢复挂起的订阅（SuspendRetries 状态，204）
func (c *Client) ResumeSubscription(ctx context.Context, subID string) error {
	_, err := c.Post(ctx, "/redfish/v1/EventService/Subscriptions/"+subID+"/Actions/EventDestination.ResumeSubscription", map[string]any{}, nil)
	return err
}

// DeleteSubscription 删除订阅（204）
func (c *Client) DeleteSubscription(ctx context.Context, subID string) error {
	return c.Delete(ctx, "/redfish/v1/EventService/Subscriptions/"+subID)
}

// ---------- 日志服务（手册 3.18–3.21） ----------

// LogService 日志服务实例（SEL/操作日志）
type LogService struct {
	ODataContext       string `json:"@odata.context"`
	ODataID            string `json:"@odata.id"`
	ODataType          string `json:"@odata.type"`
	ID                 string `json:"Id"`
	Name               string `json:"Name"`
	ServiceEnabled     *bool  `json:"ServiceEnabled"`
	MaxNumberOfRecords *int   `json:"MaxNumberOfRecords"`
	OverWritePolicy    string `json:"OverWritePolicy"` // WrapsWhenFull
	LogEntryType       string `json:"LogEntryType"`    // Event|SEL|Multiple|OEM
	DateTime           string `json:"DateTime"`
	Entries            Link   `json:"Entries"`
}

// LogEntry 日志条目（SEL 事件）
type LogEntry struct {
	ODataContext   string `json:"@odata.context"`
	ODataID        string `json:"@odata.id"`
	ODataType      string `json:"@odata.type"`
	ID             string `json:"Id"`
	Created        string `json:"Created"`
	EntryType      string `json:"EntryType"` // Event|SEL|Oem
	Severity       string `json:"Severity"`  // OK|Warning|Critical
	Message        string `json:"Message"`
	MessageId      string `json:"MessageId"`
	EntryCode      string `json:"EntryCode"`
	SensorType     string `json:"SensorType"`
	SensorNumber   *int   `json:"SensorNumber"`
	EventTimestamp string `json:"EventTimestamp"`
}

// LogService 获取日志服务实例。scope 为 Chassis/Systems/Managers 之一（默认 Chassis），
// logServiceID 通常为 "Logs"。
func (c *Client) LogService(ctx context.Context, scope, entityID, logServiceID string) (*LogService, error) {
	path, err := c.logServicePath(ctx, scope, entityID, logServiceID)
	if err != nil {
		return nil, err
	}
	var ls LogService
	if err := c.Get(ctx, path, &ls); err != nil {
		return nil, err
	}
	return &ls, nil
}

func (c *Client) logServicePath(ctx context.Context, scope, entityID, logServiceID string) (string, error) {
	if scope == "" {
		scope = "Chassis"
	}
	var collectionPath string
	switch scope {
	case "Chassis":
		id, err := c.ResolveChassisID(ctx, entityID)
		if err != nil {
			return "", err
		}
		entityID = id
		collectionPath = "/redfish/v1/Chassis/" + id + "/LogServices"
	case "Systems":
		id, err := c.ResolveSystemID(ctx, entityID)
		if err != nil {
			return "", err
		}
		entityID = id
		collectionPath = "/redfish/v1/Systems/" + id + "/LogServices"
	case "Managers":
		id, err := c.ResolveManagerID(ctx, entityID)
		if err != nil {
			return "", err
		}
		entityID = id
		collectionPath = "/redfish/v1/Managers/" + id + "/LogServices"
	default:
		return "", fmt.Errorf("redfish: 未知日志范围 %q（须为 Chassis/Systems/Managers）", scope)
	}
	if logServiceID == "" {
		var err error
		logServiceID, err = c.FirstMemberID(ctx, collectionPath, "log service")
		if err != nil {
			return "", err
		}
	}
	return collectionPath + "/" + logServiceID, nil
}

// LogEntries 列出日志条目（集合响应即含完整条目，无需逐条 GET；跟随 nextLink 翻页）
func (c *Client) LogEntries(ctx context.Context, scope, entityID, logServiceID string) ([]LogEntry, error) {
	base, err := c.logServicePath(ctx, scope, entityID, logServiceID)
	if err != nil {
		return nil, err
	}
	var out []LogEntry
	path := base + "/Entries"
	for path != "" {
		var wrapper struct {
			Members         []LogEntry `json:"Members"`
			MembersNextLink string     `json:"Members@odata.nextLink"`
		}
		if err := c.Get(ctx, path, &wrapper); err != nil {
			return nil, err
		}
		out = append(out, wrapper.Members...)
		if wrapper.MembersNextLink == "" {
			break
		}
		path = nextLinkPath(wrapper.MembersNextLink)
	}
	return out, nil
}

// SetLogServiceDateTime 设置日志服务时间/UTC 偏移（手册 3.19 可写字段，204）
func (c *Client) SetLogServiceDateTime(ctx context.Context, scope, entityID, logServiceID, dateTime, localOffset string) error {
	base, err := c.logServicePath(ctx, scope, entityID, logServiceID)
	if err != nil {
		return err
	}
	body := map[string]any{}
	if dateTime != "" {
		body["DateTime"] = dateTime
	}
	if localOffset != "" {
		body["DateTimeLocalOffset"] = localOffset
	}
	return c.Patch(ctx, base, body, nil)
}

// ClearLog 清空日志（202 → 任务 URI）
func (c *Client) ClearLog(ctx context.Context, scope, entityID, logServiceID string) (string, error) {
	base, err := c.logServicePath(ctx, scope, entityID, logServiceID)
	if err != nil {
		return "", err
	}
	return c.runAction(ctx, base+"/Actions/LogService.ClearLog", map[string]any{})
}
