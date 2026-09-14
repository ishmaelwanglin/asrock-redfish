# redfish — AST2600 BMC Redfish v1.11 全功能客户端

依据《BMC (AST2600) Generic Redfish v1.11 API User Guide Rev 1.00》（AMI 固件，2023-12）实现的 Go 库，
覆盖手册 3.2–3.126 全部资源章节与附录 FixedBootOrder，供 ops 仓库各服务（cmdb/tickets/ops-service/bootos）统一引用。

## 引用方式（ops 仓库多模块）

在调用方 `go.mod` 中加：

```
require github.com/ishmaelwanglin/asrock-redfish v0.2.0
```

import 路径与包名不同（模块名带 `asrock-`，包名是 `redfish`），建议显式起别名：

```go
import redfish "github.com/ishmaelwanglin/asrock-redfish"
```

> 本地联调本库时可在调用方 `go.mod` 临时加
> `replace github.com/ishmaelwanglin/asrock-redfish => ../../asrock-redfish`，
> **提交前必须撤掉**——留着它，版本号就成了摆设（编译实际走的是本地工作树）。

## 快速上手

```go
package main

import (
    "context"
    "fmt"
    "time"

    redfish "github.com/ishmaelwanglin/asrock-redfish"
)

func main() {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()

    // BMC 均为自签证书，须显式跳过校验（或用 WithHTTPClient 注入含自签 CA 的客户端）
    c := redfish.NewClient("192.168.1.100", "admin", "password", redfish.WithInsecureTLS(true))

    // 连通性探测
    root, _ := c.Root(ctx)
    fmt.Println("Redfish", root.RedfishVersion, root.Vendor) // 1.11.0 AMI

    // 批量操作建议改用会话认证（一次登录，后续请求带 X-Auth-Token）
    // defer c.Logout(ctx)
    // c.Login(ctx)
}
```

## 运维场景速览

所有方法第一个参数为资源 ID，**留空自动发现**（AST2600 实例名为 `Self`，其他固件可能为 `1`）。

### 电源与引导（装机/回收）

```go
c.SystemReset(ctx, "", redfish.ResetForceRestart)   // On/ForceOff/GracefulShutdown/ForceRestart/PowerCycle，202+任务
c.SystemResetAndWait(ctx, "", redfish.ResetOn)      // 操作并等待任务终态
c.SetSystemBootOnce(ctx, "", redfish.BootPxe, redfish.BootModeUEFI) // PXE 一次性引导
c.ClearBootOverride(ctx, "")
c.System(ctx, "")                                    // PowerState/SerialNumber/Model/Boot...
```

装机典型链路：`InsertMedia`（虚拟光驱挂 ISO）→ `SetSystemBootOnce(BootCd)` → `SystemReset(On)`；
裸机 PXE 装机：`SetSystemBootOnce(BootPxe)` → `SystemReset(On)`。

### 虚拟媒体

```go
c.InsertMedia(ctx, "", "CD1", redfish.InsertMediaOptions{
    Image: "//192.168.36.224/OS/ubuntu.iso", TransferProtocolType: redfish.TransferCIFS,
    UserName: "user", Password: "pass", // CIFS/OEM/HTTPS 必填
})
c.EjectMedia(ctx, "", "CD1")
```

### 硬件盘点（CMDB 采集）

```go
sys, _ := c.System(ctx, "")     // 型号/序列号/UUID/BIOS 版本/内存与处理器摘要
fru, _ := c.FRUInfo(ctx, "")    // Board/Chassis/Product 序列号
mems, _ := c.Memories(ctx, "")  // 内存条：容量/型号/槽位
cpus, _ := c.Processors(ctx, "")// CPU：型号/核数/主频
nics, _ := c.NetworkInterfaces(ctx, "")
adps, _ := c.NetworkAdapters(ctx, "")
pcie, _ := c.PCIeDevices(ctx, "") // PCIe 设备（自动翻页）
pwr, _ := c.Power(ctx, "")        // 功耗/电压/电源模块
th, _ := c.Thermal(ctx, "")       // 温度/风扇
sensors, _ := c.Sensors(ctx, "")
```

### BMC 管理

```go
c.Manager(ctx, "")                    // 固件版本/UUID
c.ResetManager(ctx, "")               // 重启 BMC
c.SetBMCStaticIPv4(ctx, "", "", "192.168.50.10", "255.255.255.0", "192.168.50.1")
c.SetBMCDHCPv4(ctx, "", "", true)
c.NetworkProtocol(ctx, "")            // HTTPS/SSH/IPMI/KVM 端口开关
c.UpdateNetworkProtocol(ctx, "", map[string]redfish.ProtocolConfig{
    "SSH": {Port: intPtr(22), ProtocolEnabled: boolPtr(true)},
}, []string{"203.107.6.88"})          // NTP 服务器
```

### 命名时区（BMC 私有 Web REST，**不是** Redfish）

Redfish 的 `Manager.TimeZoneName` 要 v1.10+，AST2600 实机 `$metadata` 只声明到 `Manager.v1_6_0`，
PATCH 直接 `400 PropertyUnknown`；而固件里「命名时区」与「手动偏移」是**同一个字符串槽**——
用 Redfish 写 `DateTimeLocalOffset` 会把 `Asia/Shanghai` 冲成 `GMT+08`，页面下拉变「请选择时区」。
要让命名时区生效，只能走 BMC 自己的 Web REST：

```go
w := redfish.NewWebClient("192.168.48.134", "admin", pass, redfish.WithInsecureTLS(true))
defer w.Logout(ctx)                                 // 每次登录占一个 BMC Web 会话槽，用完立刻还

if ok, err := w.SupportsDateTime(ctx); err != nil {
    return err                                      // 探测出错
} else if ok {                                      // SupportsDateTime 免登录、零副作用
    if err := w.Login(ctx); err != nil {
        return err                                  // ErrTFARequired / ErrPasswordChangeRequired / *WebError
    }
    res, err := w.SetTimeZone(ctx, "Asia/Shanghai", nil) // 第三参 nil = 不碰 utc_minutes
    if err != nil {
        return err
    }
    if !res.Applied {                               // 固件 200 但没采纳，res.After 是回读真值
        // 在这里降级（例如回退写偏移），别把"发出去了"当成"生效了"
    }
}
```

三条实现约束：**PUT 是全量替换语义**，所以 `SetTimeZone` 读-改-写按原始字节回送 GET 到的每个字段
（`timestamp` 经 `float64` 往返会变形），GET 响应缺 `timezone` 键时一个写请求都不发；
`Applied` 一律由**写后回读**判定；该私有 API 无跨固件兼容性承诺，只按当前 AST2600 固件行为实现。
另注意：命名时区落地后再用 Redfish PATCH `DateTimeLocalOffset`，会把它冲回 `GMT+08`。

### 固件升级

```go
c.SimpleUpdate(ctx, redfish.UpdateProtocolHTTP, "http://server/rom.ima", "", "") // 远程拉取
c.UploadImageAndWait(ctx, "/tmp/bmc.ima", []string{redfish.FirmwareBMC}, "BMC")  // 本地推送+轮询
c.UpdateServiceProgress(ctx, "")      // FlashPercentage 刷写进度
c.SetPreserveConfiguration(ctx, redfish.PreserveConfiguration{Network: true, NTP: true, REDFISH: true})
c.FirmwareInventory(ctx, "")          // BIOS/BMC/CPLD 版本清单
```

### RAID / 存储

```go
st, _ := c.Storage(ctx, "", "StorageUnit_0")
drives, _ := c.Drives(ctx, "", "StorageUnit_0")
vols, _ := c.Volumes(ctx, "", "StorageUnit_0")
c.CreateVolume(ctx, "", "StorageUnit_0", redfish.CreateVolumeOptions{
    Name: "VOL0", RAIDType: redfish.RAID1, StripSizeBytes: redfish.Strip64K,
    ReadCachePolicyType: "ReadAhead", WriteCachePolicyType: "WriteThrough",
    ControllerURI: st.ODataID, Initialization: "No_Initialization",
    DiskCachePolicy: "Unchanged", AccessPolicy: "Read-Write",
    DeviceURIs: []string{"/redfish/v1/Systems/Self/Storage/StorageUnit_0/Drives/xxx"},
})
```

### 账户与角色

```go
c.CreateAccount(ctx, "user1", "pw@123", redfish.RoleOperator, true)
c.ChangePassword(ctx, "2", "newpw@123")
c.Accounts(ctx, ""); c.Roles(ctx, "")
c.SetLockoutPolicy(ctx, 5, 600, 60)   // 失败 5 次锁 10 分钟
```

### 事件订阅 / 日志 / 遥测

```go
c.Subscribe(ctx, redfish.SubscribeOptions{Destination: "http://ops:5000/event", Context: "cmdb"})
c.SubmitTestEvent(ctx, "EventLog.1.0.ResourceAdded")
entries, _ := c.LogEntries(ctx, "", "", "") // SEL 日志（scope: Chassis/Systems/Managers）
c.ClearLog(ctx, "", "", "")
c.CreateNumericTrigger(ctx, "TempAlert", "温度告警",
    []string{redfish.TriggerActionRedfishEvent},
    redfish.NumericThresholds{UpperCritical: &redfish.Threshold{Reading: 80, Activation: "Increasing", DwellTime: "PT1M"}},
    []string{"/redfish/v1/Chassis/Self/Thermal#/Temperatures/13/ReadingCelsius"})
c.MetricReports(ctx, "")
```

### 证书

```go
csr, _ := c.GenerateCSR(ctx, redfish.CSRRequest{CommonName: "bmc.example.com"}, fmt.Sprintf(redfish.CertHTTPS, "Self"))
// csr 交 CA 签发后：
c.ReplaceCertificate(ctx, "/redfish/v1/Managers/Self/NetworkProtocol/HTTPS/Certificates/1", signedPEM)
```

### BIOS 属性 / FixedBootOrder

BIOS 启用 FBO 特性时标准 `Boot.BootOrder` 修改无效，改走 BIOS 属性：

```go
attrs, _ := c.GetBIOSAttributes(ctx, "")
fbo, _ := c.GetFixedBootOrder(ctx, "")  // FBO001 模式 + FBO1xx/FBO2xx 顺序
c.SetFixedBootOrder(ctx, "", redfish.BIOSAttributes{"FBO202": "FBO202CDDVD"})
reg, _ := c.BIOSAttributeRegistry(ctx, "BiosAttributeRegistryA2994.5.2.0") // 属性候选值
```

## 错误处理

非 2xx 响应返回 `*redfish.HTTPError`（含 Redfish error 载荷的 message 与 ExtendedInfo）：

```go
var he *redfish.HTTPError
if errors.As(err, &he) && he.StatusCode == 401 { /* 会话过期/密码错误 */ }
// 便捷判断：redfish.IsNotFound / IsUnauthorized / IsForbidden
```

Action 类操作（电源/挂载/升级）返回 202+任务，`*AndWait` 变体会轮询到终态；
升级用 `UploadImageAndWait`（容忍 BMC 重启窗口）。

### 网络引导（iSCSI 装机）

```go
funcs, _ := c.NetworkDeviceFunctions(ctx, "/redfish/v1/Chassis/Self/NetworkAdapters/NIC0/NetworkDeviceFunctions")
c.ConfigureiSCSIBoot(ctx, funcs[0].ODataID, redfish.iSCSIBootSettings{
    IPAddressType: "IPv4", InitiatorName: "iqn.2023:initiator", InitiatorIPAddress: "192.168.233.250",
    PrimaryTarget: redfish.iSCSIBootTarget{TargetName: "iqn.2023:target", TargetIPAddress: "192.168.233.2", TargetTCPPort: intPtr(3260)},
})
c.SetNetworkDeviceBootMode(ctx, funcs[0].ODataID, redfish.NDFBootiSCSI) // SD 下次启动生效
```

### 其他资源（手册全量覆盖）

```go
// 组合系统（手册 3.43–3.47）
c.CompositionService(ctx, ""); c.SetCompositionServiceEnabled(ctx, true)
blocks, _ := c.ResourceBlocks(ctx, ""); c.ReserveResourceBlock(ctx, "ComputeBlock", true)
c.ResourceZones(ctx, "")
// 网络端口与 LLDP 上联（手册 3.32–3.33）
ports, _ := c.NetworkAdapterPorts(ctx, "", "NIC0") // ports[0].Ethernet.LLDPReceive 上联交换机/端口
// PCIe 功能（VendorId/DeviceId）
c.PCIeFunctions(ctx, "", "00_01_00")
// 注册表（手册 3.69–3.73）
c.Registries(ctx, ""); c.MessageRegistry(ctx, "EventLog.1.0.0"); c.PrivilegeRegistry(ctx, "Redfish_1.0.3_PrivilegeRegistry")
c.JSONSchemas(ctx, "")
// 证书位置 / 外部账户提供者
c.CertificateLocations(ctx, ""); c.ExternalAccountProviders(ctx, "")
// 周边设备（只读）
c.MediaControllers(ctx, ""); c.GraphicsControllers(ctx, ""); c.USBControllers(ctx, "")
c.MemoryDomains(ctx, ""); c.MemoryChunks(ctx, "", "MemoryDomain_SystemMemory_0")
c.HostEthernetInterfaces(ctx, "", "Self"); c.SubProcessors(ctx, "", "DevType1_CPU0")
c.StorageControllers(ctx, "", "StorageUnit_0")
c.Signatures(ctx, "", "PK") // + DeleteSignature
// 日志服务时间
c.SetLogServiceDateTime(ctx, "", "", "", "2026-09-08T10:00:00+08:00", "+08:00")
// VLAN SD 修改（下次启动生效）
c.UpdateVLAN(ctx, vlanURI, intPtr(103), boolPtr(true), intPtr(3))
```

### AMI OEM 扩展

```go
c.UploadCABundle(ctx, "/tmp/ca.pem")      // 升级 HTTPS 拉取的 CA Bundle
c.ResetRedfishDB(ctx, "")                 // 重建 Redfish 数据库
c.SetPowerSaveMode(ctx, "", "Enable")     // BMC 省电模式
c.ConfigureCDInstance(ctx, "", 2)         // 虚拟 CD 实例数
c.EnableRMedia(ctx, "", true)             // 远程媒体开关
c.AMIConfigurations(ctx, "")              // CA 证书 URL（DHCP option 248 优先级）
c.SetCertificateAuthorityURL(ctx, "https://server/ca.pem")
c.InventoryDataStatus(ctx, "")            // BIOS 清单处理状态（盘点就绪判断）
```

## 设计要点（相对手册的行为约束）

- **任务双形态**：Reset/InsertMedia/ClearLog 直接返回 Task 资源；UpdateService 系列把任务 URI 藏在
  `Messages[Task.1.0.New].MessageArgs[0]`——`runAction` 统一解析。
- **分页**：PCIeDevices 等大集合返回 `Members@odata.nextLink`，`CollectionAll` 自动跟随。
- **RAID 创建**（AMI 扩展）响应为 204 而非标准 201；全部必填项集中在 `CreateVolumeOptions`。
- **证书**：私钥仅支持未加密 PKCS#1/PKCS#8；PEM 内换行须为 `\n`。
- **遥测固件限制**：采集周期下限 PT10M、报表上限 5 份、MetricReportDefinitionType 仅 Periodic。
- **RemoteRoleMapping**：LDAP/AD 角色映射共 5 槽，PATCH 时数组内 `null` 槽位即删除。

## 测试

```
go test ./...
```

httptest 模拟 BMC 覆盖：会话认证、自动发现、任务轮询、分页、multipart 上传、RAID/订阅/账户请求体、
iSCSI 引导配置、LLDP 端口、组合服务、CA Bundle 上传、FBO 读写、错误载荷解析等 28 组用例；
私有 Web REST 另有 24 组（`webclient_test.go`：CSRF/会话语义、全量回写逐字节、静默吞写、schema 不符零写入）。

## 手册覆盖对照

手册 3.1–3.126 全部 124 个资源章节 + 附录 FixedBootOrder 均已实现（含 AST2600 实机为空的
MediaController/CompositionService 等演示性资源，保证手册内功能无死角）。
唯一未实现的写操作是手册中未给出定义的项（如 Volume DELETE——手册 3.109 未定义该方法，
如需可用通用 `c.Delete(volumeURI)`）。
