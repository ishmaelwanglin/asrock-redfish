package redfish

import (
	"context"
	"fmt"
	"strings"
)

// ---------- UEFI 引导顺序治理（BootOptions / BootOrder / Settings(FutureState)） ----------
//
// 实测结论（AMI AST2600 / BMC_fw 1.20.00 / BIOS 10.38.TR01 真机，2026-10）：
//
//  1. 一次性引导覆盖（BootSourceOverrideTarget=Pxe / Enabled=Once）会被固件**消费，
//     但不改变引导顺序**：机器仍按 NVRAM 里的既有顺序启动。要真正做到「下次从 PXE 起」，
//     只能持久改写 UEFI BootOrder（SetSystemBootToNetwork）。
//  2. BootOrder 数组里的元素是引导项在顺序表里的**名字**（AST2600 形态 "Boot000E"），
//     不是 BootOptions 成员的 Id（"000E"）。写错名字固件照样 2xx 却不落盘
//     ⇒ 写入后必须**回读校验**（本文件所有写路径都回读）。
//  3. AST2600 把 Boot 属性迁到 Settings(FutureState) 资源
//     /redfish/v1/Systems/<id>/SD，直写主资源会 400
//     ("Support of this Operation for Boot Properties is moved to FutureState URI(...)")；
//     写 SD 时固件**强制要求** BootSourceOverrideTarget/BootSourceOverrideEnabled/
//     BootSourceOverrideMode 三件套，缺一 400。
//  4. AST2600 对 PATCH 强制要求 If-Match（缺失返回 428 Ami.1.0.PreconditionHeaderMissing）。
//  5. SD(Settings) 里的 BootOrder 是「BMC 在下次 reset 时下发给固件的 pending 顺序」，
//     主资源里的才是「固件当前生效的 NVRAM 值」。判断「下次开机会按什么顺序启动」以 SD 为准；
//     两者不一致是常态（BootOS agent 用 efibootmgr 直改 NVRAM 时尤其明显）。

// BootOrderState 引导顺序治理结果
type BootOrderState struct {
	// NeedsRestart 改写后是否需要一次重启才能让固件消费新顺序。
	// 只写了未来状态(SD/FutureState)时固件要等下次启动才消费 ⇒ 需要重启；
	// 未改写（配置本就正确）时为 false。
	NeedsRestart bool
	// Written 是否改写过引导顺序（未来状态 SD 或主资源）。
	Written bool
	// Entry 被提到首位的引导项在 BootOrder 里的元素名（诊断用）。
	Entry string
	// EffectiveFirst / FutureFirst 固件生效顺序、未来状态顺序的首位元素名（诊断用）。
	EffectiveFirst string
	FutureFirst    string
	// ConfiguredFirst 「下次开机会生效的顺序」首位元素名（SD 优先，否则生效顺序）。
	ConfiguredFirst string
}

// BootOrderView 引导顺序视图（区分固件生效顺序与未来状态顺序）
type BootOrderView struct {
	// SettingsPath Settings(FutureState) 资源路径（该固件没有则空串）
	SettingsPath string
	// Effective 主资源上的顺序 = 固件当前生效的 NVRAM 值
	Effective []string
	// Future SD 资源上的顺序 = BMC 在下次 reset 时下发给固件的 pending 顺序
	Future []string
	// Configured 「下次开学会生效的顺序」（Future 优先，否则 Effective）
	Configured []string
}

// SystemBootSettingsPath 定位系统的 Settings(FutureState) 资源路径。
// 标准注解：@Redfish.Settings.SettingsObject.@odata.id（AST2600 指向 /redfish/v1/Systems/<id>/SD）。
// 该固件没有该资源时返回空串。
func (c *Client) SystemBootSettingsPath(ctx context.Context, systemID string) (string, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return "", err
	}
	return c.bootSettingsPath(ctx, "/redfish/v1/Systems/"+id), nil
}

// SystemBootOrderView 读取引导顺序视图（区分固件生效顺序 / 未来状态顺序）。
func (c *Client) SystemBootOrderView(ctx context.Context, systemID string) (BootOrderView, error) {
	var v BootOrderView
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return v, err
	}
	sysPath := "/redfish/v1/Systems/" + id
	v.SettingsPath = c.bootSettingsPath(ctx, sysPath)
	v.Effective = c.bootOrderAt(ctx, sysPath)
	if v.SettingsPath != "" {
		v.Future = c.bootOrderAt(ctx, v.SettingsPath)
	}
	v.Configured = v.Future
	if len(v.Configured) == 0 {
		v.Configured = v.Effective
	}
	return v, nil
}

// SystemBootOverrideMode 判定引导模式（UEFI / Legacy）。
//
// 判据：SecureBoot 资源存在 = UEFI 固件（该资源只有 UEFI 固件才暴露；
// AST2600 上 GET /redfish/v1/Systems/<id>/SecureBoot 返回 200）；
// SecureBoot 404 = Legacy BIOS 固件。
//
// 其它错误（网络/认证/固件不支持）**不猜模式**：猜错会「写成功但引导不上」而查不出来，
// 故如实报错，让调用方重试或人工确认。
func (c *Client) SystemBootOverrideMode(ctx context.Context, systemID string) (string, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return "", err
	}
	return c.bootOverrideMode(ctx, "/redfish/v1/Systems/"+id)
}

// SetSystemBootToNetwork 把网络(PXE/HTTP)引导项提到 BootOrder 首位并落盘（持久改写）。
//
// preferMAC：可选的期望引导网卡 MAC，用于在多个网卡引导项中精确选中；
// 为空或匹配不到时回退到首个网络引导项（挑不出则如实报错，不猜）。
//
// 写 Settings(FutureState) 资源优先（AST2600 的 Boot 属性都在那里，且强制要求
// BootSourceOverrideTarget/Enabled/Mode 三件套，缺一 400）；失败回退写主资源。
// 写完回读确认首位确为目标项，不一致即报错（固件常 2xx 却不落盘）。
func (c *Client) SetSystemBootToNetwork(ctx context.Context, systemID, preferMAC string) (BootOrderState, error) {
	var st BootOrderState
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return st, err
	}
	sysPath := "/redfish/v1/Systems/" + id
	sdPath := c.bootSettingsPath(ctx, sysPath)

	opts, err := c.BootOptions(ctx, systemID)
	if err != nil {
		return st, err
	}
	netOpt, err := PickNetworkBootOption(opts, preferMAC)
	if err != nil {
		return st, err
	}

	effective := c.bootOrderAt(ctx, sysPath)
	var future []string
	if sdPath != "" {
		future = c.bootOrderAt(ctx, sdPath)
	}
	if len(effective) > 0 {
		st.EffectiveFirst = effective[0]
	}
	if len(future) > 0 {
		st.FutureFirst = future[0]
	}
	base := future
	if len(base) == 0 {
		base = effective
	}
	if len(base) == 0 {
		return st, fmt.Errorf("读取当前 BootOrder 失败（SD 与主资源均未返回）")
	}
	st.ConfiguredFirst = base[0]

	entry := ResolveBootEntry(netOpt, append(append([]string{}, effective...), future...))
	if entry == "" {
		return st, fmt.Errorf("无法把网络引导项 %s 映射到当前 BootOrder 元素（当前顺序: %v）", netOpt.DisplayName, base)
	}
	st.Entry = entry

	newOrder := PromoteBootEntry(base, entry)
	if len(newOrder) == 0 {
		return st, fmt.Errorf("当前 BootOrder 为空，无法把网络引导项 %s 提到首位", entry)
	}
	if len(newOrder) == len(base) && newOrder[0] == base[0] {
		// 已在首位：不改写，仍回读确认，避免「以为写好了」
		if err := c.verifyBootOrderFirst(ctx, sdPath, sysPath, entry); err != nil {
			return st, err
		}
		return st, nil
	}
	if err := c.writeBootOrder(ctx, sysPath, sdPath, "", newOrder, entry); err != nil {
		return st, err
	}
	st.Written = true
	st.NeedsRestart = true
	return st, nil
}

// SetSystemBootToDisk 把本地磁盘引导项提到 BootOrder 首位并落盘（持久改写）。
//
// 语义：把磁盘引导项提到 BootOrder 首位，其余项保持原有相对顺序。
// 区分「固件生效顺序」（主资源 = BMC 视图）与「未来状态」（SD = BMC 在 reset 时
// 下发给固件的 pending 顺序）：
//   - SD 才是「下次开机会生效的顺序」，判据以它为准（SD 缺失时退回主资源）；
//   - 该顺序已是磁盘项置首 ⇒ 不改写（是否重启由调用方按场景决定，
//     例如收口路径不应再重启：BootOS agent 已自行治理 NVRAM 并重启）；
//   - 否则写未来状态（SD 优先，失败回退主资源）并回读校验 ⇒ NeedsRestart=true。
func (c *Client) SetSystemBootToDisk(ctx context.Context, systemID string) (BootOrderState, error) {
	var st BootOrderState
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return st, err
	}
	sysPath := "/redfish/v1/Systems/" + id
	sdPath := c.bootSettingsPath(ctx, sysPath)

	// 固件生效顺序（NVRAM 当前值）：主资源。读不到即如实报错，不猜。
	effective := c.bootOrderAt(ctx, sysPath)
	if len(effective) == 0 {
		return st, fmt.Errorf("读取固件生效引导顺序失败（主资源未返回 Boot.BootOrder）")
	}
	st.EffectiveFirst = effective[0]

	var future []string
	if sdPath != "" {
		future = c.bootOrderAt(ctx, sdPath)
		if len(future) > 0 {
			st.FutureFirst = future[0]
		}
	}

	opts, err := c.BootOptions(ctx, systemID)
	if err != nil {
		return st, err
	}
	diskOpt, err := PickDiskBootOption(opts)
	if err != nil {
		return st, err
	}

	// BootOrder 里的元素是「引导项在顺序表里的名字」，各家形态不同：
	// AST2600 的 BootOptions 成员 Id="000E"、Name="Boot000E"、DisplayName="ubuntu"，
	// 而 BootOrder 数组里是 "Boot000E" ⇒ 直接拿 Id 去写会被固件忽略（回读原顺序）。
	// 故以「当前 BootOrder 数组」为准反查该引导项的元素名（生效与未来状态合并查）。
	entry := ResolveBootEntry(diskOpt, append(append([]string{}, effective...), future...))
	if entry == "" {
		return st, fmt.Errorf("无法把磁盘引导项 %s 映射到当前 BootOrder 元素（生效顺序: %v，未来状态: %v）", diskOpt.DisplayName, effective, future)
	}
	st.Entry = entry

	// 「下次开机会生效的顺序」：SD 优先（AST2600 的 pending 顺序在 SD），否则用主资源。
	configured := future
	if len(configured) == 0 {
		configured = effective
	}
	st.ConfiguredFirst = configured[0]
	if configured[0] == entry {
		// 配置（本将要生效的顺序）已是磁盘项置首 ⇒ 无需改写。
		return st, nil
	}

	newOrder := PromoteBootEntry(configured, entry)
	if len(newOrder) == 0 {
		return st, fmt.Errorf("当前 BootOrder 为空，无法把磁盘引导项 %s 提到首位", entry)
	}
	if err := c.writeBootOrder(ctx, sysPath, sdPath, "", newOrder, entry); err != nil {
		return st, err
	}
	st.Written = true
	st.NeedsRestart = true
	return st, nil
}

// SetSystemBootOverrideOnce 设置一次性引导覆盖（下次启动生效后自动失效）。
//
// 先按历史 payload（不带 BootSourceOverrideMode，部分厂商拒收该字段）写主资源；
// 失败则定位 Settings(FutureState) 资源并补齐 mode（AST2600 把 Boot 属性迁到 SD，
// 且 SD 强制要求 mode），写完回读校验（固件常 2xx 却不写盘）。
//
// 注意：一次性覆盖**不改变引导顺序**（AST2600 实测：被消费但机器仍按 NVRAM 既有顺序启动），
// 需要真正改引导顺序请用 SetSystemBootToNetwork / SetSystemBootToDisk。
func (c *Client) SetSystemBootOverrideOnce(ctx context.Context, systemID, target, mode string) error {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	sysPath := "/redfish/v1/Systems/" + id

	// 主路径：历史 payload（不带 BootSourceOverrideMode）
	mainErr := c.PatchWithIfMatch(ctx, sysPath, bootOverridePayload(target, ""))
	if mainErr == nil {
		return nil
	}

	// 回退：固件把 Boot 属性迁到 Settings(FutureState) 资源
	sdPath := c.bootSettingsPath(ctx, sysPath)
	if sdPath == "" {
		return mainErr
	}
	if mode == "" {
		m, modeErr := c.bootOverrideMode(ctx, sysPath)
		if modeErr != nil {
			return fmt.Errorf("主路径设置引导失败: %w；且无法判定引导模式: %w", mainErr, modeErr)
		}
		mode = m
	}
	// 首选判定出的模式；固件拒收该模式时再试另一种（各固件允许值不同）
	var sdErr error
	usedMode := ""
	for _, m := range []string{mode, otherBootMode(mode)} {
		if err := c.PatchWithIfMatch(ctx, sdPath, bootOverridePayload(target, m)); err != nil {
			sdErr = err
			continue
		}
		sdErr = nil
		usedMode = m
		break
	}
	if sdErr != nil {
		return fmt.Errorf("主路径设置引导失败: %w；Settings 路径 %s 设置也失败: %w", mainErr, sdPath, sdErr)
	}
	// 回读校验：SD 是「未来状态」，主资源不回显，只能读 SD 自身
	if err := c.verifyBootOverride(ctx, sdPath, target, usedMode); err != nil {
		return fmt.Errorf("引导已写入 Settings 路径 %s，但回读未生效: %w", sdPath, err)
	}
	return nil
}

// ---------- 内部实现 ----------

// bootSettingsPath 定位 Settings(FutureState) 资源路径；取不到返回空串。
func (c *Client) bootSettingsPath(ctx context.Context, sysPath string) string {
	var raw map[string]any
	if err := c.Get(ctx, sysPath, &raw); err != nil {
		return ""
	}
	settings, ok := raw["@Redfish.Settings"].(map[string]any)
	if !ok {
		return ""
	}
	obj, ok := settings["SettingsObject"].(map[string]any)
	if !ok {
		return ""
	}
	path, _ := obj["@odata.id"].(string)
	return path
}

// bootOverrideMode 判定引导模式（UEFI / Legacy），判据见 SystemBootOverrideMode。
func (c *Client) bootOverrideMode(ctx context.Context, sysPath string) (string, error) {
	var probe map[string]any
	err := c.Get(ctx, sysPath+"/SecureBoot", &probe)
	if err == nil {
		return BootModeUEFI, nil
	}
	if IsNotFound(err) {
		return BootModeLegacy, nil
	}
	return "", fmt.Errorf("无法判定引导模式（SecureBoot 探测失败，既非 200 也非 404）: %w", err)
}

// bootOrderAt 读取指定资源上的 Boot.BootOrder 数组（读不到返回空切片）。
func (c *Client) bootOrderAt(ctx context.Context, path string) []string {
	var raw map[string]any
	if err := c.Get(ctx, path, &raw); err != nil {
		return nil
	}
	boot, ok := raw["Boot"].(map[string]any)
	if !ok {
		return nil
	}
	arr, ok := boot["BootOrder"].([]any)
	if !ok {
		return nil
	}
	out := []string{}
	for _, v := range arr {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// writeBootOrder 持久改写引导顺序：优先写 Settings(FutureState)，失败回退主资源；写完回读校验。
//
// 写 SD 时补齐三件套（BootSourceOverrideTarget/Enabled/Mode），AST2600 缺一即 400；
// 写主资源只写 BootOrder（其它厂商不认那三个字段）。
func (c *Client) writeBootOrder(ctx context.Context, sysPath, sdPath, mode string, newOrder []string, entry string) error {
	if sdPath != "" {
		if mode == "" {
			m, err := c.bootOverrideMode(ctx, sysPath)
			if err != nil {
				return fmt.Errorf("无法判定引导模式: %w", err)
			}
			mode = m
		}
		sdPayload := map[string]any{"Boot": map[string]any{
			"BootOrder":                 newOrder,
			"BootSourceOverrideTarget":  BootNone,
			"BootSourceOverrideEnabled": BootOverrideDisabled,
			"BootSourceOverrideMode":    mode,
		}}
		sdErr := c.PatchWithIfMatch(ctx, sdPath, sdPayload)
		if sdErr == nil {
			return c.verifyBootOrderAt(ctx, sdPath, entry)
		}
		mainErr := c.PatchWithIfMatch(ctx, sysPath, map[string]any{"Boot": map[string]any{"BootOrder": newOrder}})
		if mainErr != nil {
			return fmt.Errorf("写引导顺序失败：SD 路径 %s: %v；主资源路径: %w", sdPath, sdErr, mainErr)
		}
		return c.verifyBootOrderAt(ctx, sysPath, entry)
	}
	// 无 SD 资源：直接写主资源
	if err := c.PatchWithIfMatch(ctx, sysPath, map[string]any{"Boot": map[string]any{"BootOrder": newOrder}}); err != nil {
		return fmt.Errorf("写引导顺序失败: %w", err)
	}
	return c.verifyBootOrderAt(ctx, sysPath, entry)
}

// verifyBootOrderAt 回读**指定资源**上的 BootOrder 并校验首位是目标元素。
// 写入路径与校验路径必须一致：固件常 2xx 却不落盘，回读是唯一可信判据。
func (c *Client) verifyBootOrderAt(ctx context.Context, path, entry string) error {
	order := c.bootOrderAt(ctx, path)
	if len(order) == 0 {
		return fmt.Errorf("回读引导顺序失败（%s 未返回 Boot.BootOrder，期望引导项 %s 居首）", path, entry)
	}
	if order[0] != entry {
		return fmt.Errorf("回读引导顺序首位=%v，期望引导项 %s 居首", order, entry)
	}
	return nil
}

// verifyBootOrderFirst 回读引导顺序（SD 优先，其次主资源）并校验首位是目标元素。
func (c *Client) verifyBootOrderFirst(ctx context.Context, sdPath, sysPath, entry string) error {
	paths := []string{}
	if sdPath != "" {
		paths = append(paths, sdPath)
	}
	paths = append(paths, sysPath)
	for _, p := range paths {
		order := c.bootOrderAt(ctx, p)
		if len(order) == 0 {
			continue
		}
		if order[0] != entry {
			return fmt.Errorf("回读引导顺序首位=%v，期望引导项 %s 居首", order, entry)
		}
		return nil
	}
	return fmt.Errorf("回读引导顺序失败（SD 与主资源均未返回 Boot.BootOrder）")
}

// verifyBootOverride 回读 Settings 资源，确认引导覆盖确实落盘
// （固件常返回 2xx 却不写盘，只看状态码会把「没生效」报成成功）。
func (c *Client) verifyBootOverride(ctx context.Context, sdPath, wantTarget, wantMode string) error {
	var raw map[string]any
	if err := c.Get(ctx, sdPath, &raw); err != nil {
		return fmt.Errorf("回读 %s 失败: %w", sdPath, err)
	}
	boot, ok := raw["Boot"].(map[string]any)
	if !ok {
		return fmt.Errorf("%s 无 Boot 段", sdPath)
	}
	target, _ := boot["BootSourceOverrideTarget"].(string)
	enabled, _ := boot["BootSourceOverrideEnabled"].(string)
	if target != wantTarget {
		return fmt.Errorf("%s 的 BootSourceOverrideTarget=%q（期望 %q）", sdPath, target, wantTarget)
	}
	if enabled != BootOverrideOnce && enabled != BootOverrideContinuous {
		return fmt.Errorf("%s 的 BootSourceOverrideEnabled=%q（未生效）", sdPath, enabled)
	}
	// 固件若回显 mode，必须与实际写入的一致（防止「写 UEFI 却落成 Legacy」）
	if got, _ := boot["BootSourceOverrideMode"].(string); got != "" && wantMode != "" && got != wantMode {
		return fmt.Errorf("%s 的 BootSourceOverrideMode=%q（期望 %q）", sdPath, got, wantMode)
	}
	return nil
}

// bootOverridePayload 组装引导覆盖 payload；mode 为空表示不下发 BootSourceOverrideMode。
func bootOverridePayload(target, mode string) map[string]any {
	boot := map[string]any{
		"BootSourceOverrideTarget":  target,
		"BootSourceOverrideEnabled": BootOverrideOnce,
	}
	if mode != "" {
		boot["BootSourceOverrideMode"] = mode
	}
	return map[string]any{"Boot": boot}
}

// otherBootMode 返回另一种引导模式（UEFI ↔ Legacy），用于固件拒收首选模式时回退。
func otherBootMode(mode string) string {
	if mode == BootModeUEFI {
		return BootModeLegacy
	}
	return BootModeUEFI
}

// ---------- 引导项挑选与元素名解析（包级函数，便于调用方自行组合策略） ----------

// PickDiskBootOption 从引导项集合里挑出本地磁盘引导项。
//
// 判据优先级：
//  1. UefiDevicePath 以 "HD(" 开头（UEFI 硬盘分区路径）——最硬的磁盘特征；
//  2. 其次按名称排除网络项（含 HTTP/PXE/IPv4/IPv6/Shell/Setup/Diags 即判为非磁盘）。
//
// 挑不出时**如实报错**，不猜：写错引导顺序会让机器下次启动进不去系统。
func PickDiskBootOption(opts []*BootOption) (*BootOption, error) {
	if len(opts) == 0 {
		return nil, fmt.Errorf("引导项集合为空，无法定位本地磁盘引导项")
	}
	for _, o := range opts {
		if strings.HasPrefix(o.UefiDevicePath, "HD(") {
			return o, nil
		}
	}
	for _, o := range opts {
		if IsNetworkBootOption(o) {
			continue
		}
		if o.DisplayName != "" || o.Name != "" {
			return o, nil
		}
	}
	return nil, fmt.Errorf("引导项集合中未找到本地磁盘引导项（共 %d 项，均为网络/其它引导项）", len(opts))
}

// PickNetworkBootOption 从引导项集合里挑出网络(PXE/HTTP)引导项。
//
//  1. preferMAC 精确匹配：BootOptions 成员的 UefiDevicePath 含 MAC(...)，如
//     "…/MAC(9C6B004A3817,0x1)/IPv4(…)"，据此锁定指定网卡（PXE 引导口）的引导项；
//  2. 回退：优先 DisplayName/Name 含 PXE 的，其次含 HTTP/IPv4 的；
//  3. 仍挑不出则**如实报错**，不猜：写错引导顺序会让机器下次启动进不去系统。
func PickNetworkBootOption(opts []*BootOption, preferMAC string) (*BootOption, error) {
	if len(opts) == 0 {
		return nil, fmt.Errorf("引导项集合为空，无法定位网络(PXE)引导项")
	}
	norm := NormalizeMAC(preferMAC)
	if norm != "" {
		for _, o := range opts {
			if DevicePathHasMAC(o.UefiDevicePath, norm) {
				return o, nil
			}
		}
	}
	for _, kw := range []string{"pxe", "http", "ipv4"} {
		for _, o := range opts {
			if !IsNetworkBootOption(o) {
				continue
			}
			if strings.Contains(strings.ToLower(o.DisplayName+" "+o.Name), kw) {
				return o, nil
			}
		}
	}
	for _, o := range opts {
		if IsNetworkBootOption(o) {
			return o, nil
		}
	}
	return nil, fmt.Errorf("引导项集合中未找到网络(PXE/HTTP)引导项（共 %d 项）", len(opts))
}

// IsNetworkBootOption 判断引导项是否网络引导（PXE / HTTP 引导）。
func IsNetworkBootOption(o *BootOption) bool {
	name := strings.ToLower(o.Name + " " + o.DisplayName + " " + o.UefiDevicePath)
	for _, kw := range []string{"pxe", "http", "ipv4", "ipv6", "shell", "setup", "diags", "efi shell"} {
		if strings.Contains(name, kw) {
			return true
		}
	}
	return false
}

// ResolveBootEntry 把一个引导项映射成它在当前 BootOrder 数组里的元素名。
//
// 各家固件形态不同：AST2600 的 BootOptions 成员 Id="000E"、Name="Boot000E"、
// DisplayName="ubuntu"，而 BootOrder 数组里放的是 "Boot000E"。直接拿 Id 去写，
// 固件会当成无效项忽略（PATCH 2xx 但顺序不变）。故以当前 BootOrder 数组为准反查：
// 先按 Name 精确匹配，再按 Id 精确匹配，再按 "Boot"+Id 后缀匹配，最后才回退 Name/Id。
func ResolveBootEntry(opt *BootOption, order []string) string {
	if opt == nil {
		return ""
	}
	for _, e := range order {
		if opt.Name != "" && strings.EqualFold(e, opt.Name) {
			return e
		}
	}
	for _, e := range order {
		if opt.ID != "" && strings.EqualFold(e, opt.ID) {
			return e
		}
	}
	if opt.ID != "" {
		for _, e := range order {
			if strings.EqualFold(strings.TrimPrefix(strings.ToLower(e), "boot"), strings.ToLower(opt.ID)) {
				return e
			}
		}
	}
	if opt.Name != "" {
		return opt.Name
	}
	return opt.ID
}

// PromoteBootEntry 把指定引导项提到首位，其余项保持原有相对顺序。
func PromoteBootEntry(order []string, entry string) []string {
	out := []string{}
	if entry != "" {
		out = append(out, entry)
	}
	for _, o := range order {
		if o == entry {
			continue
		}
		out = append(out, o)
	}
	return out
}

// NormalizeMAC 归一化 MAC（去掉分隔符、大写），便于与设备路径里的 MAC(...) 比对。
// 长度不足 12 位（非法 MAC）时返回空串。
func NormalizeMAC(mac string) string {
	mac = strings.ToUpper(strings.TrimSpace(mac))
	mac = strings.ReplaceAll(mac, ":", "")
	mac = strings.ReplaceAll(mac, "-", "")
	mac = strings.ReplaceAll(mac, ".", "")
	if len(mac) != 12 {
		return ""
	}
	return mac
}

// DevicePathHasMAC 判断 UEFI 设备路径里是否含指定 MAC（传入未归一化的 MAC 亦可）。
func DevicePathHasMAC(devicePath, mac string) bool {
	norm := NormalizeMAC(mac)
	if norm == "" || devicePath == "" {
		return false
	}
	return strings.Contains(strings.ToUpper(devicePath), "MAC("+norm)
}
