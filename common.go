package redfish

import "encoding/json"

// ---------- 通用模型（手册 3.1 ODATA and Frequently Used Properties） ----------

// Link @odata.id 引用
type Link struct {
	ODataID string `json:"@odata.id"`
}

// Status 资源状态（State/Health 枚举见手册 Table 4）
type Status struct {
	State        string `json:"State"`
	Health       string `json:"Health"`
	HealthRollup string `json:"HealthRollup"`
}

// State 枚举（手册 Table 4）
const (
	StateEnabled        = "Enabled"
	StateDisabled       = "Disabled"
	StateStandbyOffline = "StandbyOffline"
	StateStandbySpare   = "StandbySpare"
	StateInTest         = "InTest"
	StateStarting       = "Starting"
	StateAbsent         = "Absent"
	StateUnavailable    = "UnavailableOffline"
	StateDeferring      = "Deferring"
	StateQuiesced       = "Quiesced"
	StateUpdating       = "Updating"
)

// Health 枚举
const (
	HealthOK       = "OK"
	HealthWarning  = "Warning"
	HealthCritical = "Critical"
)

// IndicatorLED 指示灯枚举（Lit/Blinking/Off）
const (
	IndicatorLEDLit      = "Lit"
	IndicatorLEDBlinking = "Blinking"
	IndicatorLEDOff      = "Off"
)

// ResetType 电源操作枚举（手册 Table 4 Reset + Chassis.Reset/ComputerSystem.Reset）
const (
	ResetOn               = "On"               // 开机
	ResetForceOff         = "ForceOff"         // 立即断电（非优雅）
	ResetGracefulShutdown = "GracefulShutdown" // 优雅关机
	ResetForceRestart     = "ForceRestart"     // 强制重启
	ResetPowerCycle       = "PowerCycle"       // 断电循环
)

// PowerState 电源状态
const (
	PowerOn  = "On"
	PowerOff = "Off"
)

// BootSourceOverrideTarget 一次性引导目标枚举（手册 Table 103，完整 15 值）
const (
	BootNone         = "None"
	BootPxe          = "Pxe"
	BootFloppy       = "Floppy"
	BootCd           = "Cd"
	BootUsb          = "Usb"
	BootHdd          = "Hdd"
	BootBiosSetup    = "BiosSetup"
	BootUtilities    = "Utilities"
	BootDiags        = "Diags"
	BootUefiShell    = "UefiShell"
	BootUefiTarget   = "UefiTarget"
	BootSDCard       = "SDCard"
	BootUefiHttp     = "UefiHttp"
	BootRemoteDrive  = "RemoteDrive"
	BootUefiBootNext = "UefiBootNext"
)

// BootSourceOverrideEnabled 枚举
const (
	BootOverrideDisabled   = "Disabled"
	BootOverrideOnce       = "Once"
	BootOverrideContinuous = "Continuous"
)

// BootSourceOverrideMode 枚举
const (
	BootModeLegacy = "Legacy"
	BootModeUEFI   = "UEFI"
)

// PowerRestorePolicy 来电恢复策略（手册 Table 102）
const (
	RestoreAlwaysOn  = "AlwaysOn"
	RestoreAlwaysOff = "AlwaysOff"
	RestoreLastState = "LastState"
)

// OemBlock OEM 扩展对象原样保留（各资源 Oem.Ami 字段差异大，强类型在具体资源定义）
type OemBlock map[string]json.RawMessage

// Identifier 持久标识（手册 Table 5）
type Identifier struct {
	DurableName       string `json:"DurableName"`
	DurableNameFormat string `json:"DurableNameFormat"`
}

// StateCondition State 枚举的中文提示（日志/展示用）
func DescribeState(state string) string {
	switch state {
	case StateEnabled:
		return "已启用"
	case StateDisabled:
		return "已禁用"
	case StateAbsent:
		return "不在位"
	case StateUnavailable:
		return "在位但不可用"
	case StateStarting:
		return "启动中"
	case StateInTest:
		return "测试中"
	case StateUpdating:
		return "升级中"
	default:
		return state
	}
}
