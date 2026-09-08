package redfish

import (
	"context"
	"fmt"
)

// ---------- BIOS 属性（手册附录 4.1 FixedBootOrder） ----------
// AST2600 固件未提供独立 Bios 章节的属性表，但附录确认：
//   GET  /redfish/v1/Systems/{id}/Bios     → 当前 BIOS 属性（含 FBO101..FBO20x）
//   PATCH /redfish/v1/Systems/{id}/Bios/SD → 修改 BIOS 属性（下次启动生效）

// BIOSAttributes BIOS 属性键值对（值类型不定：字符串/数字/布尔）
type BIOSAttributes map[string]any

// GetBIOSAttributes 获取当前 BIOS 属性（含 FixedBootOrder FBO1xx/FBO2xx）
func (c *Client) GetBIOSAttributes(ctx context.Context, systemID string) (BIOSAttributes, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Attributes BIOSAttributes `json:"Attributes"`
	}
	if err := c.Get(ctx, "/redfish/v1/Systems/"+id+"/Bios", &resp); err != nil {
		return nil, err
	}
	return resp.Attributes, nil
}

// SetBIOSAttributes 修改 BIOS 属性（PATCH Bios/SD，204，下次启动生效）
func (c *Client) SetBIOSAttributes(ctx context.Context, systemID string, attrs BIOSAttributes) error {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	return c.Patch(ctx, "/redfish/v1/Systems/"+id+"/Bios/SD",
		map[string]any{"Attributes": attrs}, nil)
}

// AttributeRegistryEntry 属性注册表条目（手册 Table 91）
type AttributeRegistryEntry struct {
	AttributeName string `json:"AttributeName"`
	DisplayName   string `json:"DisplayName"`
	HelpText      string `json:"HelpText"`
	CurrentValue  any    `json:"CurrentValue"`
	DefaultValue  any    `json:"DefaultValue"`
	ReadOnly      *bool  `json:"ReadOnly"`
	Type          string `json:"Type"` // Enumeration|String|Integer|Boolean|Password
	Value         []struct {
		ValueName        string `json:"ValueName"`
		ValueDisplayName string `json:"ValueDisplayName"`
	} `json:"Value"`
}

// AttributeRegistry BIOS 属性注册表
type AttributeRegistry struct {
	ODataContext    string `json:"@odata.context"`
	ODataID         string `json:"@odata.id"`
	ID              string `json:"Id"`
	Name            string `json:"Name"`
	RegistryVersion string `json:"RegistryVersion"`
	RegistryEntries struct {
		Attributes []AttributeRegistryEntry `json:"Attributes"`
	} `json:"RegistryEntries"`
}

// BIOSAttributeRegistry 获取 BIOS 属性注册表（属性元数据：候选值/只读/帮助文本）。
// registryID 形如 "BiosAttributeRegistryA2994.5.2.0"（从 /redfish/v1/Registries 列表获得）。
func (c *Client) BIOSAttributeRegistry(ctx context.Context, registryID string) (*AttributeRegistry, error) {
	var ar AttributeRegistry
	if err := c.Get(ctx, "/redfish/v1/Registries/"+registryID+".json", &ar); err != nil {
		return nil, err
	}
	return &ar, nil
}

// ---------- FixedBootOrder（FBO）辅助（手册附录 4.1） ----------
// BIOS 启用 FBO 后标准 Boot.BootOrder PATCH 无效，须读写 BIOS 属性：
//   FBO001      引导模式（UEFI/Legacy）
//   FBO101-10x  Legacy 启动顺序
//   FBO201-20x  UEFI 启动顺序
// 候选值在属性注册表 FBO1xx/FBO2xx 条目的 Value[] 中。

// FixedBootOrder 当前 FBO 启动顺序
type FixedBootOrder struct {
	Mode     string   // FBO001 的值（UEFI/Legacy）
	Legacy   []string // FBO101.. 值（FBO101HardDisk 等）
	UEFI     []string // FBO201.. 值
	AllAttrs BIOSAttributes
}

// GetFixedBootOrder 读取当前 FBO 启动顺序（从 BIOS 属性中提取 FBO1xx/FBO2xx）
func (c *Client) GetFixedBootOrder(ctx context.Context, systemID string) (*FixedBootOrder, error) {
	attrs, err := c.GetBIOSAttributes(ctx, systemID)
	if err != nil {
		return nil, err
	}
	fbo := &FixedBootOrder{AllAttrs: attrs}
	for k, v := range attrs {
		s, ok := v.(string)
		if !ok {
			continue
		}
		switch {
		case k == "FBO001":
			fbo.Mode = s
		case len(k) == 6 && k[:3] == "FBO" && k[3] == '1':
			fbo.Legacy = append(fbo.Legacy, s)
		case len(k) == 6 && k[:3] == "FBO" && k[3] == '2':
			fbo.UEFI = append(fbo.UEFI, s)
		}
	}
	return fbo, nil
}

// SetFixedBootOrder 修改 FBO 启动顺序（PATCH Bios/SD，204）。
// 仅传需变更的槽位，例：{"FBO202": "FBO202CDDVD", "FBO203": "FBO203NVME"}
func (c *Client) SetFixedBootOrder(ctx context.Context, systemID string, slots BIOSAttributes) error {
	return c.SetBIOSAttributes(ctx, systemID, slots)
}

// ---------- SecureBoot（手册 3.96–3.100） ----------

// ResetKeysType 枚举
const (
	ResetAllKeysToDefault = "ResetAllKeysToDefault"
	DeleteAllKeys         = "DeleteAllKeys"
)

// SecureBoot 安全启动资源
type SecureBoot struct {
	ODataContext          string `json:"@odata.context"`
	ODataID               string `json:"@odata.id"`
	ODataType             string `json:"@odata.type"`
	ID                    string `json:"Id"`
	Name                  string `json:"Name"`
	SecureBootEnable      *bool  `json:"SecureBootEnable"`
	SecureBootCurrentBoot string `json:"SecureBootCurrentBoot"` // Enabled|Disabled
	SecureBootMode        string `json:"SecureBootMode"`        // SetupMode|UserMode|AuditMode|DeployedMode
	SecureBootDatabases   Link   `json:"SecureBootDatabases"`
	Status                Status `json:"Status"`
}

// SecureBoot 获取安全启动状态
func (c *Client) SecureBoot(ctx context.Context, systemID string) (*SecureBoot, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	var sb SecureBoot
	if err := c.Get(ctx, "/redfish/v1/Systems/"+id+"/SecureBoot", &sb); err != nil {
		return nil, err
	}
	return &sb, nil
}

// SetSecureBootEnable 开关 UEFI Secure Boot（204）
func (c *Client) SetSecureBootEnable(ctx context.Context, systemID string, enable bool) error {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	return c.Patch(ctx, "/redfish/v1/Systems/"+id+"/SecureBoot",
		map[string]bool{"SecureBootEnable": enable}, nil)
}

// SecureBootResetKeys 重置/删除安全启动密钥库（204）。
// scope 为空重置全部库；否则仅重置指定库（PK/KEK/db/dbx/dbr/dbt）。
func (c *Client) SecureBootResetKeys(ctx context.Context, systemID, databaseID, resetKeysType string) error {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	body := map[string]string{"ResetKeysType": resetKeysType}
	if databaseID == "" {
		_, err := c.Post(ctx, "/redfish/v1/Systems/"+id+"/SecureBoot/Actions/SecureBoot.ResetKeys", body, nil)
		return err
	}
	_, err = c.Post(ctx, fmt.Sprintf("/redfish/v1/Systems/%s/SecureBoot/SecureBootDatabases/%s/Actions/SecureBootDatabase.ResetKeys", id, databaseID), body, nil)
	return err
}

// Signature 安全启动签名条目
type Signature struct {
	ODataContext          string `json:"@odata.context"`
	ODataID               string `json:"@odata.id"`
	ID                    string `json:"Id"`
	Name                  string `json:"Name"`
	SignatureString       string `json:"SignatureString"`
	SignatureType         string `json:"SignatureType"` // EFI_CERT_SHA256_GUID 等
	SignatureTypeRegistry string `json:"SignatureTypeRegistry"`
	UefiSignatureOwner    string `json:"UefiSignatureOwner"`
}

// SecureBootDatabases 全部安全启动库名（PK/KEK/db/dbx/dbr/dbt 及其 Default 变体）
func (c *Client) SecureBootDatabases(ctx context.Context, systemID string) ([]string, error) {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return nil, err
	}
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Systems/"+id+"/SecureBoot/SecureBootDatabases")
	if err != nil {
		return nil, err
	}
	return coll.MemberIDs(), nil
}

// AddSignature 向安全启动库添加签名（仅非 Default 库可写，201）
func (c *Client) AddSignature(ctx context.Context, systemID, databaseID, signatureString, signatureType, signatureTypeRegistry, uefiSignatureOwner string) error {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	body := map[string]string{
		"SignatureString":       signatureString,
		"SignatureType":         signatureType,
		"SignatureTypeRegistry": signatureTypeRegistry,
		"UEFISignatureOwner":    uefiSignatureOwner,
	}
	_, err = c.Post(ctx, fmt.Sprintf("/redfish/v1/Systems/%s/SecureBoot/SecureBootDatabases/%s/Signatures", id, databaseID), body, nil)
	return err
}

// ---------- AmiBios.ChangeState 内存安全状态（手册 3.86 POST Action） ----------

// MemoryChangeState 修改内存安全状态（Enabled/Disabled/Locked/Unlocked/Frozen/Passphraselimit，204）
func (c *Client) MemoryChangeState(ctx context.Context, systemID, memoryID, state string) error {
	id, err := c.ResolveSystemID(ctx, systemID)
	if err != nil {
		return err
	}
	_, err = c.Post(ctx, fmt.Sprintf("/redfish/v1/Systems/%s/Memory/%s/Actions/AmiBios.ChangeState", id, memoryID),
		map[string]string{"State": state}, nil)
	return err
}
