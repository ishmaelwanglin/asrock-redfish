package redfish

import (
	"context"
)

// ---------- AccountService（手册 3.3） ----------

// RemoteRoleMapping LDAP/AD 角色映射槽位（共 5 槽，null 槽位表示删除）
type RemoteRoleMapping struct {
	LocalRole   string `json:"LocalRole,omitempty"`
	RemoteGroup string `json:"RemoteGroup,omitempty"`
	RemoteUser  string `json:"RemoteUser,omitempty"`
	Oem         *struct {
		Ami struct {
			KVMAccess    string `json:"KVMAccess,omitempty"`
			VMediaAccess string `json:"VMediaAccess,omitempty"`
		} `json:"Ami,omitempty"`
	} `json:"Oem,omitempty"`
}

// LDAPServiceSearchSettings LDAP 检索设置
type LDAPServiceSearchSettings struct {
	BaseDistinguishedNames []string `json:"BaseDistinguishedNames,omitempty"`
	GroupNameAttribute     string   `json:"GroupNameAttribute,omitempty"`
	GroupsAttribute        string   `json:"GroupsAttribute,omitempty"`
	UsernameAttribute      string   `json:"UsernameAttribute,omitempty"`
}

// LDAPSettings LDAP 服务配置（PATCH AccountService.LDAP 对象）
type LDAPSettings struct {
	ServiceEnabled   *bool    `json:"ServiceEnabled,omitempty"`
	ServiceAddresses []string `json:"ServiceAddresses,omitempty"`
	Authentication   struct {
		Username string `json:"Username,omitempty"`
		Password string `json:"Password,omitempty"`
	} `json:"Authentication,omitempty"`
	LDAPService struct {
		SearchSettings LDAPServiceSearchSettings `json:"SearchSettings,omitempty"`
	} `json:"LDAPService,omitempty"`
	RemoteRoleMapping []map[string]any `json:"RemoteRoleMapping,omitempty"` // 元素可为 nil（槽位删除语义）
}

// ADSettings Active Directory 配置（PATCH AccountService.ActiveDirectory 对象）
type ADSettings struct {
	ServiceEnabled *bool `json:"ServiceEnabled,omitempty"`
	Authentication struct {
		Username string `json:"Username,omitempty"`
		Password string `json:"Password,omitempty"`
		Oem      struct {
			Ami struct {
				DomainName                  string `json:"DomainName,omitempty"`
				DomainControllerServerAddr1 string `json:"DomainControllerServerAddr1,omitempty"`
				DomainControllerServerAddr2 string `json:"DomainControllerServerAddr2,omitempty"`
				DomainControllerServerAddr3 string `json:"DomainControllerServerAddr3,omitempty"`
			} `json:"Ami,omitempty"`
		} `json:"Oem,omitempty"`
	} `json:"Authentication,omitempty"`
	RemoteRoleMapping []map[string]any `json:"RemoteRoleMapping,omitempty"`
}

// AccountService 账户服务资源
type AccountService struct {
	ODataContext                      string `json:"@odata.context"`
	ODataID                           string `json:"@odata.id"`
	ODataType                         string `json:"@odata.type"`
	ID                                string `json:"Id"`
	Name                              string `json:"Name"`
	ServiceEnabled                    *bool  `json:"ServiceEnabled"`
	AuthFailureLoggingThreshold       *int   `json:"AuthFailureLoggingThreshold"`
	AccountLockoutThreshold           *int   `json:"AccountLockoutThreshold"`
	AccountLockoutDuration            *int   `json:"AccountLockoutDuration"`
	AccountLockoutCounterResetAfter   *int   `json:"AccountLockoutCounterResetAfter"`
	AccountLockoutCounterResetEnabled *bool  `json:"AccountLockoutCounterResetEnabled"`
	MinPasswordLength                 *int   `json:"MinPasswordLength"`
	MaxPasswordLength                 *int   `json:"MaxPasswordLength"`
	Accounts                          Link   `json:"Accounts"`
	Roles                             Link   `json:"Roles"`
}

// AccountService 获取账户服务（含密码长度限制/锁定策略）
func (c *Client) AccountService(ctx context.Context) (*AccountService, error) {
	var as AccountService
	if err := c.Get(ctx, "/redfish/v1/AccountService", &as); err != nil {
		return nil, err
	}
	return &as, nil
}

// UpdateAccountService 更新账户服务设置（锁定策略等，204）
func (c *Client) UpdateAccountService(ctx context.Context, body map[string]any) error {
	return c.Patch(ctx, "/redfish/v1/AccountService", body, nil)
}

// SetLockoutPolicy 设置账户锁定策略（失败 N 次锁定 M 秒；duration=0 不锁定）
func (c *Client) SetLockoutPolicy(ctx context.Context, threshold, durationSeconds, resetAfterSeconds int) error {
	return c.UpdateAccountService(ctx, map[string]any{
		"AccountLockoutThreshold":           threshold,
		"AccountLockoutDuration":            durationSeconds,
		"AccountLockoutCounterResetAfter":   resetAfterSeconds,
		"AccountLockoutCounterResetEnabled": true,
	})
}

// ConfigureLDAP 配置 LDAP 外部认证（PATCH AccountService.LDAP，204）
func (c *Client) ConfigureLDAP(ctx context.Context, settings LDAPSettings) error {
	return c.Patch(ctx, "/redfish/v1/AccountService", map[string]any{"LDAP": settings}, nil)
}

// ConfigureActiveDirectory 配置 AD 外部认证（PATCH AccountService.ActiveDirectory，204）
func (c *Client) ConfigureActiveDirectory(ctx context.Context, settings ADSettings) error {
	return c.Patch(ctx, "/redfish/v1/AccountService", map[string]any{"ActiveDirectory": settings}, nil)
}

// ---------- ManagerAccount 账户（手册 3.4–3.5） ----------

// SNMPCredentials AMI SNMP 账户扩展
type SNMPCredentials struct {
	AuthenticationProtocol string `json:"AuthenticationProtocol"` // SHA256|SHA384|SHA512
	EncryptionProtocol     string `json:"EncryptionProtocol"`     // DES|AES
	Access                 string `json:"Access"`                 // ReadOnly|ReadWrite
}

// Account BMC 本地账户
type Account struct {
	ODataContext           string   `json:"@odata.context"`
	ODataID                string   `json:"@odata.id"`
	ODataType              string   `json:"@odata.type"`
	ID                     string   `json:"Id"`
	Name                   string   `json:"Name"`
	UserName               string   `json:"UserName"`
	RoleId                 string   `json:"RoleId"`
	Enabled                *bool    `json:"Enabled"`
	Locked                 *bool    `json:"Locked"`
	AccountTypes           []string `json:"AccountTypes"` // Redfish|SNMP
	PasswordChangeRequired *bool    `json:"PasswordChangeRequired"`
	Oem                    struct {
		Ami struct {
			SNMP SNMPCredentials `json:"SNMP"`
		} `json:"Ami"`
	} `json:"Oem"`
	Links struct {
		Role Link `json:"Role"`
	} `json:"Links"`
}

// Accounts 列出全部本地账户
func (c *Client) Accounts(ctx context.Context) ([]*Account, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/AccountService/Accounts")
	if err != nil {
		return nil, err
	}
	out := make([]*Account, 0, len(coll.Members))
	for _, m := range coll.Members {
		var a Account
		if err := c.Get(ctx, m.ODataID, &a); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, nil
}

// CreateAccount 创建账户（201 返回新账户）。roleID: Administrator|Operator|ReadOnly|自定义角色。
func (c *Client) CreateAccount(ctx context.Context, userName, password, roleID string, enabled bool) (*Account, error) {
	body := map[string]any{
		"Name":     userName + " Account",
		"UserName": userName,
		"Password": password,
		"RoleId":   roleID,
		"Enabled":  enabled,
		"Locked":   false,
	}
	var a Account
	if _, err := c.Post(ctx, "/redfish/v1/AccountService/Accounts", body, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// CreateSNMPAccount 创建带 SNMP 扩展的账户（手册 3.4.4.2 变体 2）
func (c *Client) CreateSNMPAccount(ctx context.Context, userName, password, roleID string, snmp SNMPCredentials) (*Account, error) {
	body := map[string]any{
		"Name":         userName + " Account",
		"UserName":     userName,
		"Password":     password,
		"RoleId":       roleID,
		"Enabled":      true,
		"Locked":       false,
		"AccountTypes": []string{"Redfish", "SNMP"},
		"Oem":          map[string]any{"Ami": map[string]any{"SNMP": snmp}},
	}
	var a Account
	if _, err := c.Post(ctx, "/redfish/v1/AccountService/Accounts", body, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// UpdateAccount 更新账户（用户名/密码/角色/启停/解锁/SNMP 凭证按需传，204）
func (c *Client) UpdateAccount(ctx context.Context, accountID string, body map[string]any) error {
	return c.Patch(ctx, "/redfish/v1/AccountService/Accounts/"+accountID, body, nil)
}

// ChangePassword 修改账户密码
func (c *Client) ChangePassword(ctx context.Context, accountID, newPassword string) error {
	return c.UpdateAccount(ctx, accountID, map[string]any{"Password": newPassword})
}

// DeleteAccount 删除账户（204）
func (c *Client) DeleteAccount(ctx context.Context, accountID string) error {
	return c.Delete(ctx, "/redfish/v1/AccountService/Accounts/"+accountID)
}

// ---------- Role 角色（手册 3.12–3.13） ----------

// Redfish 标准权限（手册 Table 2）
const (
	PrivLogin               = "Login"
	PrivConfigureManager    = "ConfigureManager"
	PrivConfigureUsers      = "ConfigureUsers"
	PrivConfigureSelf       = "ConfigureSelf"
	PrivConfigureComponents = "ConfigureComponents"
)

// 预定义角色
const (
	RoleAdministrator = "Administrator"
	RoleOperator      = "Operator"
	RoleReadOnly      = "ReadOnly"
)

// Role 角色资源
type Role struct {
	ODataContext       string   `json:"@odata.context"`
	ODataID            string   `json:"@odata.id"`
	ODataType          string   `json:"@odata.type"`
	ID                 string   `json:"Id"`
	Name               string   `json:"Name"`
	RoleId             string   `json:"RoleId"`
	IsPredefined       *bool    `json:"IsPredefined"`
	AssignedPrivileges []string `json:"AssignedPrivileges"`
	OemPrivileges      []string `json:"OemPrivileges"`
}

// Roles 列出全部角色
func (c *Client) Roles(ctx context.Context) ([]*Role, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/AccountService/Roles")
	if err != nil {
		return nil, err
	}
	out := make([]*Role, 0, len(coll.Members))
	for _, m := range coll.Members {
		var r Role
		if err := c.Get(ctx, m.ODataID, &r); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, nil
}

// CreateRole 创建自定义角色（AssignedPrivileges 与 OemPrivileges 至少其一，201）
func (c *Client) CreateRole(ctx context.Context, roleID string, assignedPrivileges, oemPrivileges []string) (*Role, error) {
	body := map[string]any{
		"Id":                 roleID,
		"RoleId":             roleID,
		"Name":               roleID + " Role",
		"AssignedPrivileges": assignedPrivileges,
	}
	if oemPrivileges != nil {
		body["OemPrivileges"] = oemPrivileges
	}
	var r Role
	if _, err := c.Post(ctx, "/redfish/v1/AccountService/Roles", body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// UpdateRole 更新角色权限（204）
func (c *Client) UpdateRole(ctx context.Context, roleID string, assignedPrivileges []string) error {
	return c.Patch(ctx, "/redfish/v1/AccountService/Roles/"+roleID,
		map[string]any{"AssignedPrivileges": assignedPrivileges}, nil)
}

// DeleteRole 删除自定义角色（204）
func (c *Client) DeleteRole(ctx context.Context, roleID string) error {
	return c.Delete(ctx, "/redfish/v1/AccountService/Roles/"+roleID)
}

// ---------- RADIUS / PAM（手册 3.9–3.11） ----------

// RADIUSSettings RADIUS 外部认证
type RADIUSSettings struct {
	ODataContext        string `json:"@odata.context"`
	ODataID             string `json:"@odata.id"`
	ID                  string `json:"Id"`
	AccountProviderType string `json:"AccountProviderType"`
	ServiceEnabled      *bool  `json:"ServiceEnabled"`
	Oem                 struct {
		Ami struct {
			ServiceAddress string `json:"ServiceAddress"`
			ServicePort    *int   `json:"ServicePort"`
			Secret         string `json:"Secret"`
			Timeout        *int   `json:"Timeout"`
		} `json:"Ami"`
	} `json:"Oem"`
}

// RADIUSSettings 获取 RADIUS 配置
func (c *Client) RADIUSSettings(ctx context.Context) (*RADIUSSettings, error) {
	var r RADIUSSettings
	if err := c.Get(ctx, "/redfish/v1/AccountService/ExternalAccountProviders/RADIUS", &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ConfigureRADIUS 配置 RADIUS（服务器地址/端口/共享密钥，204）
func (c *Client) ConfigureRADIUS(ctx context.Context, enabled bool, server string, port int, secret string) error {
	body := map[string]any{
		"ServiceEnabled": enabled,
		"Oem": map[string]any{
			"Ami": map[string]any{
				"ServiceAddress": server,
				"ServicePort":    port,
				"Secret":         secret,
			},
		},
	}
	return c.Patch(ctx, "/redfish/v1/AccountService/ExternalAccountProviders/RADIUS", body, nil)
}

// ConfigureRADIUSAuthorization 配置 RADIUS 特权级别映射（手册 3.10，H=4 管理员..H=0 无权限）
func (c *Client) ConfigureRADIUSAuthorization(ctx context.Context, administrator, operator, user, oem, noAccess string) error {
	body := map[string]any{
		"RADIUSAuthorization": map[string]string{
			"Administrator": administrator,
			"Operator":      operator,
			"User":          user,
			"Oem":           oem,
			"NoAccess":      noAccess,
		},
	}
	return c.Patch(ctx, "/redfish/v1/AccountService/ExternalAccountProviders/RADIUS/Oem/Ami/AdvancedRADIUSSetting", body, nil)
}

// PAMConfiguration PAM 认证顺序配置（OEM AMI，手册 3.11）
type PAMConfiguration struct {
	ODataContext string   `json:"@odata.context"`
	ODataID      string   `json:"@odata.id"`
	ID           string   `json:"Id"`
	PAMEnabled   *bool    `json:"PAMEnabled"`
	PAMOrder     []string `json:"PAMOrder"` // LDAP|IPMI|"ACTIVE DIRECTORY"|RADIUS
}

// PAMConfiguration 获取 PAM 配置
func (c *Client) PAMConfiguration(ctx context.Context) (*PAMConfiguration, error) {
	var p PAMConfiguration
	if err := c.Get(ctx, "/redfish/v1/AccountService/Oem/Ami/Configurations", &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ConfigurePAM 配置认证走 PAM 及模块顺序（204）
func (c *Client) ConfigurePAM(ctx context.Context, enabled bool, order []string) error {
	return c.Patch(ctx, "/redfish/v1/AccountService/Oem/Ami/Configurations",
		map[string]any{"PAMEnabled": enabled, "PAMOrder": order}, nil)
}
