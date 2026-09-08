package redfish

import (
	"context"
)

// ---------- CertificateService 证书服务（手册 3.6–3.7、3.14–3.15） ----------

// 证书集合路径（手册 3.6：五处证书集合）
const (
	CertHTTPS      = "/redfish/v1/Managers/%s/NetworkProtocol/HTTPS/Certificates" // managerID
	CertLDAPServer = "/redfish/v1/AccountService/LDAP/Certificates"
	CertLDAPClient = "/redfish/v1/AccountService/LDAP/Certificates/Oem/Ami/ClientCertificates"
	CertSMTP       = "/redfish/v1/EventService/Oem/Ami/SMTP/Certificates"
	CertBoot       = "/redfish/v1/Systems/%s/Boot/Certificates" // systemID
)

// Certificate 证书实例
type Certificate struct {
	ODataContext      string `json:"@odata.context"`
	ODataID           string `json:"@odata.id"`
	ODataType         string `json:"@odata.type"`
	ID                string `json:"Id"`
	Name              string `json:"Name"`
	CertificateString string `json:"CertificateString"` // PEM（不含私钥）
	CertificateType   string `json:"CertificateType"`   // PEM
	ValidNotBefore    string `json:"ValidNotBefore"`
	ValidNotAfter     string `json:"ValidNotAfter"`
	Issuer            struct {
		CommonName string `json:"CommonName"`
	} `json:"Issuer"`
	Subject struct {
		CommonName string `json:"CommonName"`
	} `json:"Subject"`
	KeyUsage []string `json:"KeyUsage"`
}

// CSRRequest 生成 CSR 请求（手册 3.14.4.2 Table 20）
type CSRRequest struct {
	Country            string   `json:"Country,omitempty"` // 两位国家码
	State              string   `json:"State,omitempty"`
	City               string   `json:"City,omitempty"`
	Organization       string   `json:"Organization,omitempty"`
	OrganizationalUnit string   `json:"OrganizationalUnit,omitempty"`
	CommonName         string   `json:"CommonName"` // 必填，FQDN 或 IP
	AlternativeNames   []string `json:"AlternativeNames,omitempty"`
	Email              string   `json:"Email,omitempty"`
	KeyBitLength       int      `json:"KeyBitLength,omitempty"` // 512–2048，默认 2048
	KeyUsage           []string `json:"KeyUsage,omitempty"`
	ChallengePassword  string   `json:"ChallengePassword,omitempty"`
	GivenName          string   `json:"GivenName,omitempty"`
	Initials           string   `json:"Initials,omitempty"`
	Surname            string   `json:"Surname,omitempty"`
	ContactPerson      string   `json:"ContactPerson,omitempty"`
	UnstructuredName   string   `json:"UnstructuredName,omitempty"`
}

type csrRequestWire struct {
	CSRRequest
	CertificateCollection Link `json:"CertificateCollection"`
}

// GenerateCSR 生成 CSR（200 同步返回 CSRString）。
// certCollectionURI 为目标证书集合 URI（CertHTTPS 等）。
func (c *Client) GenerateCSR(ctx context.Context, req CSRRequest, certCollectionURI string) (csrPEM string, err error) {
	body := csrRequestWire{CSRRequest: req, CertificateCollection: Link{ODataID: certCollectionURI}}
	var resp struct {
		CSRString             string `json:"CSRString"`
		CertificateCollection Link   `json:"CertificateCollection"`
	}
	if _, err := c.Post(ctx, "/redfish/v1/CertificateService/Actions/CertificateService.GenerateCSR", body, &resp); err != nil {
		return "", err
	}
	return resp.CSRString, nil
}

// InstallCertificate 安装证书到集合（POST CertificateString=PEM 证书+私钥，201）。
// 私钥须为未加密 PKCS#1/PKCS#8，PEM 内换行须为 \n。
func (c *Client) InstallCertificate(ctx context.Context, collectionURI, pemWithKey string) (*Certificate, error) {
	body := map[string]string{
		"CertificateString": pemWithKey,
		"CertificateType":   "PEM",
	}
	var cert Certificate
	if _, err := c.Post(ctx, collectionURI, body, &cert); err != nil {
		return nil, err
	}
	return &cert, nil
}

// ReplaceCertificate 用 CA 签发的新证书替换现有证书（CertificateService Action，204）。
// certificateURI 为被替换证书的 @odata.id。
func (c *Client) ReplaceCertificate(ctx context.Context, certificateURI, pemCertWithKey string) error {
	body := map[string]any{
		"CertificateString": pemCertWithKey,
		"CertificateType":   "PEM",
		"CertificateUri":    map[string]string{"@odata.id": certificateURI},
	}
	_, err := c.Post(ctx, "/redfish/v1/CertificateService/Actions/CertificateService.ReplaceCertificate", body, nil)
	return err
}

// Certificates 列出集合内证书
func (c *Client) Certificates(ctx context.Context, collectionURI string) ([]*Certificate, error) {
	coll, err := c.CollectionAll(ctx, collectionURI)
	if err != nil {
		return nil, err
	}
	out := make([]*Certificate, 0, len(coll.Members))
	for _, m := range coll.Members {
		var cert Certificate
		if err := c.Get(ctx, m.ODataID, &cert); err != nil {
			return nil, err
		}
		out = append(out, &cert)
	}
	return out, nil
}

// DeleteCertificate 删除证书（204）
func (c *Client) DeleteCertificate(ctx context.Context, certificateURI string) error {
	return c.Delete(ctx, certificateURI)
}

// RekeyCertificate 生成新密钥对并返回新 CSR（200）。
// keyBitLength 512–2048（0=默认 2048）。
func (c *Client) RekeyCertificate(ctx context.Context, certificateURI string, keyBitLength int, challengePassword string) (csrPEM string, err error) {
	body := map[string]any{}
	if keyBitLength > 0 {
		body["KeyBitLength"] = keyBitLength
	}
	if challengePassword != "" {
		body["ChallengePassword"] = challengePassword
	}
	var resp struct {
		CSRString   string `json:"CSRString"`
		Certificate string `json:"Certificate"`
	}
	if _, err := c.Post(ctx, certificateURI+"/Actions/Certificate.Rekey", body, &resp); err != nil {
		return "", err
	}
	return resp.CSRString, nil
}

// RenewCertificate 用现有密钥对生成续期 CSR（200）
func (c *Client) RenewCertificate(ctx context.Context, certificateURI, challengePassword string) (csrPEM string, err error) {
	body := map[string]any{}
	if challengePassword != "" {
		body["ChallengePassword"] = challengePassword
	}
	var resp struct {
		CSRString   string `json:"CSRString"`
		Certificate string `json:"Certificate"`
	}
	if _, err := c.Post(ctx, certificateURI+"/Actions/Certificate.Renew", body, &resp); err != nil {
		return "", err
	}
	return resp.CSRString, nil
}

// SMTPCertPair 一组 SMTP 证书材料
type SMTPCertPair struct {
	CACert     string // CA 证书 PEM 全文
	Cert       string // 客户端证书 PEM 全文
	PrivateKey string // 私钥 PEM 全文（不回显）
}

// InstallSMTPCertificates 安装 SMTP StartTLS 证书（AMI OEM，手册 3.51；POST 201）。
// secondary 非 nil 时同时安装备用 SMTP 证书。
func (c *Client) InstallSMTPCertificates(ctx context.Context, primary SMTPCertPair, secondary *SMTPCertPair) error {
	body := map[string]any{
		"PrimarySMTP": map[string]string{
			"CACert":     primary.CACert,
			"Cert":       primary.Cert,
			"PrivateKey": primary.PrivateKey,
		},
	}
	if secondary != nil {
		body["SecondarySMTP"] = map[string]string{
			"CACert":     secondary.CACert,
			"Cert":       secondary.Cert,
			"PrivateKey": secondary.PrivateKey,
		}
	}
	_, err := c.Post(ctx, CertSMTP, body, nil)
	return err
}

// ---------- ServiceRoot（手册 3.2） ----------

// ServiceRoot 服务根
type ServiceRoot struct {
	ODataContext   string `json:"@odata.context"`
	ODataID        string `json:"@odata.id"`
	ODataType      string `json:"@odata.type"`
	RedfishVersion string `json:"RedfishVersion"` // 如 1.11.0
	Product        string `json:"Product"`
	Vendor         string `json:"Vendor"`
	UUID           string `json:"UUID"`

	AccountService     Link `json:"AccountService"`
	CertificateService Link `json:"CertificateService"`
	Chassis            Link `json:"Chassis"`
	CompositionService Link `json:"CompositionService"`
	EventService       Link `json:"EventService"`
	Managers           Link `json:"Managers"`
	SessionService     Link `json:"SessionService"`
	Systems            Link `json:"Systems"`
	TaskService        Link `json:"TaskService"`
	TelemetryService   Link `json:"TelemetryService"`
	UpdateService      Link `json:"UpdateService"`
	Registries         Link `json:"Registries"`
	Links              struct {
		Sessions Link `json:"Sessions"`
	} `json:"Links"`
}

// Root 获取服务根（连通性探测/能力发现）
func (c *Client) Root(ctx context.Context) (*ServiceRoot, error) {
	var r ServiceRoot
	if err := c.Get(ctx, "/redfish/v1/", &r); err != nil {
		return nil, err
	}
	return &r, nil
}
