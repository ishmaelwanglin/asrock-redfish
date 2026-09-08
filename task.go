package redfish

import (
	"context"
	"fmt"
	"time"
)

// ---------- 任务服务（手册 3.112–3.114） ----------
// Action 类操作（Reset/ClearLog/InsertMedia/SimpleUpdate 等）普遍返回 202 + Task，
// 须轮询任务直至终态。

// TaskState 枚举（手册 Table 131）
const (
	TaskNew         = "New"
	TaskStarting    = "Starting"
	TaskRunning     = "Running"
	TaskSuspended   = "Suspended"
	TaskInterrupted = "Interrupted"
	TaskPending     = "Pending"
	TaskStopping    = "Stopping"
	TaskCompleted   = "Completed"
	TaskKilled      = "Killed"
	TaskException   = "Exception"
	TaskService     = "Service"
)

// TaskMessage 任务关联消息
type TaskMessage struct {
	ODataType   string   `json:"@odata.type"`
	Message     string   `json:"Message"`
	MessageArgs []string `json:"MessageArgs"`
	MessageID   string   `json:"MessageId"`
	Resolution  string   `json:"Resolution"`
	Severity    string   `json:"Severity"`
}

// Task 任务实例
type Task struct {
	ODataContext    string        `json:"@odata.context"`
	ODataID         string        `json:"@odata.id"`
	ODataType       string        `json:"@odata.type"`
	ID              string        `json:"Id"`
	Name            string        `json:"Name"`
	Description     string        `json:"Description"`
	TaskState       string        `json:"TaskState"`
	TaskStatus      string        `json:"TaskStatus"`
	StartTime       string        `json:"StartTime"`
	EndTime         string        `json:"EndTime"`
	PercentComplete *int          `json:"PercentComplete"`
	Messages        []TaskMessage `json:"Messages"`
}

// Done 任务是否到达终态
func (t *Task) Done() bool {
	switch t.TaskState {
	case TaskCompleted, TaskKilled, TaskException, TaskInterrupted:
		return true
	}
	return false
}

// OK 任务是否成功完成
func (t *Task) OK() bool { return t.TaskState == TaskCompleted && t.TaskStatus == "OK" }

// Task 获取任务详情
func (c *Client) Task(ctx context.Context, taskID string) (*Task, error) {
	var t Task
	if err := c.Get(ctx, "/redfish/v1/TaskService/Tasks/"+taskID, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Tasks 列出全部任务
func (c *Client) Tasks(ctx context.Context) ([]*Task, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/TaskService/Tasks")
	if err != nil {
		return nil, err
	}
	out := make([]*Task, 0, len(coll.Members))
	for _, m := range coll.Members {
		var t Task
		if err := c.Get(ctx, m.ODataID, &t); err != nil {
			return nil, err
		}
		out = append(out, &t)
	}
	return out, nil
}

// DeleteTask 删除任务（204）
func (c *Client) DeleteTask(ctx context.Context, taskID string) error {
	return c.Delete(ctx, "/redfish/v1/TaskService/Tasks/"+taskID)
}

// WaitTask 轮询任务直至终态。taskURI 为任务的 @odata.id（完整路径）。
// interval 为轮询间隔（<=0 时取 2s）；总时长由 ctx 控制（超时返回 ctx.Err()）。
// 任务终态非 Completed 时返回错误并携带任务详情。
func (c *Client) WaitTask(ctx context.Context, taskURI string, interval time.Duration) (*Task, error) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		var t Task
		if err := c.Get(ctx, taskURI, &t); err != nil {
			return nil, err
		}
		if t.Done() {
			if !t.OK() {
				msg := ""
				if len(t.Messages) > 0 {
					msg = t.Messages[0].Message
				}
				return &t, fmt.Errorf("redfish: 任务 %s 终态 %s/%s: %s", taskURI, t.TaskState, t.TaskStatus, msg)
			}
			return &t, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// actionResponse 兼容两种 202 响应形态：
//  1. 直接返回 Task 资源（ComputerSystem.Reset/VirtualMedia.InsertMedia/ClearLog 等，含 @odata.id）
//  2. UpdateService 风格：返回原资源 + Messages[]，任务 URI 在 Task.1.0.New 消息的 MessageArgs[0]
type actionResponse struct {
	ODataID   string        `json:"@odata.id"`
	TaskState string        `json:"TaskState"`
	Messages  []TaskMessage `json:"Messages"`
}

// runAction 执行 Action 并解析出任务 URI（无任务时返回空串）。
func (c *Client) runAction(ctx context.Context, path string, body any) (taskURI string, err error) {
	var ar actionResponse
	if _, err = c.Post(ctx, path, body, &ar); err != nil {
		return "", err
	}
	if ar.ODataID != "" && ar.TaskState != "" {
		return ar.ODataID, nil // 形态 1：Task 资源
	}
	for _, m := range ar.Messages {
		if m.MessageID == "Task.1.0.New" && len(m.MessageArgs) > 0 {
			return m.MessageArgs[0], nil // 形态 2：UpdateService Messages
		}
	}
	return "", nil // 204/200 无任务（如 EjectMedia）
}
