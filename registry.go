package redfish

import (
	"context"
	"encoding/json"
)

// ---------- CompositionService 组合服务（手册 3.43–3.47） ----------

// CompositionService 组合服务
type CompositionService struct {
	ODataContext   string `json:"@odata.context"`
	ODataID        string `json:"@odata.id"`
	ID             string `json:"Id"`
	Name           string `json:"Name"`
	ServiceEnabled *bool  `json:"ServiceEnabled"`
	ResourceBlocks Link   `json:"ResourceBlocks"`
	ResourceZones  Link   `json:"ResourceZones"`
	Status         Status `json:"Status"`
}

// CompositionService 获取组合服务
func (c *Client) CompositionService(ctx context.Context) (*CompositionService, error) {
	var cs CompositionService
	if err := c.Get(ctx, "/redfish/v1/CompositionService", &cs); err != nil {
		return nil, err
	}
	return &cs, nil
}

// SetCompositionServiceEnabled 开关组合服务（204）
func (c *Client) SetCompositionServiceEnabled(ctx context.Context, enabled bool) error {
	return c.Patch(ctx, "/redfish/v1/CompositionService", map[string]bool{"ServiceEnabled": enabled}, nil)
}

// ResourceBlock 资源块（ComputeBlock/DrivesBlock/NetworkBlock）
type ResourceBlock struct {
	ODataContext      string   `json:"@odata.context"`
	ODataID           string   `json:"@odata.id"`
	ID                string   `json:"Id"`
	Name              string   `json:"Name"`
	ResourceBlockType []string `json:"ResourceBlockType"` // Compute|Storage|Network
	CompositionStatus struct {
		CompositionState     string `json:"CompositionState"` // Composed|Unused
		Reserved             *bool  `json:"Reserved"`
		SharingCapable       *bool  `json:"SharingCapable"`
		SharingEnabled       *bool  `json:"SharingEnabled"`
		MaxCompositions      *int   `json:"MaxCompositions"`
		NumberOfCompositions *int   `json:"NumberOfCompositions"`
	} `json:"CompositionStatus"`
	Processors         []Link `json:"Processors"`
	Memory             []Link `json:"Memory"`
	Storage            []Link `json:"Storage"`
	SimpleStorage      []Link `json:"SimpleStorage"`
	Drives             []Link `json:"Drives"`
	EthernetInterfaces []Link `json:"EthernetInterfaces"`
	Status             Status `json:"Status"`
}

// ResourceBlocks 列出全部资源块（CreateComposedSystem 的入参来源）
func (c *Client) ResourceBlocks(ctx context.Context) ([]*ResourceBlock, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/CompositionService/ResourceBlocks")
	if err != nil {
		return nil, err
	}
	out := make([]*ResourceBlock, 0, len(coll.Members))
	for _, m := range coll.Members {
		var rb ResourceBlock
		if err := c.Get(ctx, m.ODataID, &rb); err != nil {
			return nil, err
		}
		out = append(out, &rb)
	}
	return out, nil
}

// ReserveResourceBlock 预留/释放资源块（PATCH CompositionStatus.Reserved，204）
func (c *Client) ReserveResourceBlock(ctx context.Context, blockID string, reserved bool) error {
	return c.Patch(ctx, "/redfish/v1/CompositionService/ResourceBlocks/"+blockID,
		map[string]any{"CompositionStatus": map[string]bool{"Reserved": reserved}}, nil)
}

// ResourceZone 资源区
type ResourceZone struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	Status       Status `json:"Status"`
	Links        struct {
		ResourceBlocks []Link `json:"ResourceBlocks"`
	} `json:"Links"`
}

// ResourceZones 列出全部资源区
func (c *Client) ResourceZones(ctx context.Context) ([]*ResourceZone, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/CompositionService/ResourceZones")
	if err != nil {
		return nil, err
	}
	out := make([]*ResourceZone, 0, len(coll.Members))
	for _, m := range coll.Members {
		var rz ResourceZone
		if err := c.Get(ctx, m.ODataID, &rz); err != nil {
			return nil, err
		}
		out = append(out, &rz)
	}
	return out, nil
}

// ---------- CertificateLocations 证书位置（手册 3.15，只读） ----------

// CertificateLocations 全机已安装证书位置 + SMTP 证书占位
type CertificateLocations struct {
	ODataContext string `json:"@odata.context"`
	ODataID      string `json:"@odata.id"`
	Links        struct {
		Certificates []Link `json:"Certificates"`
		Oem          struct {
			Ami struct {
				PrimarySMTP   json.RawMessage `json:"PrimarySMTP"`
				SecondarySMTP json.RawMessage `json:"SecondarySMTP"`
			} `json:"Ami"`
		} `json:"Oem"`
	} `json:"Links"`
}

// CertificateLocations 获取全部已安装证书位置
func (c *Client) CertificateLocations(ctx context.Context) (*CertificateLocations, error) {
	var cl CertificateLocations
	if err := c.Get(ctx, "/redfish/v1/CertificateService/CertificateLocations", &cl); err != nil {
		return nil, err
	}
	return &cl, nil
}

// ---------- ExternalAccountProviders 集合（手册 3.8，只读） ----------

// ExternalAccountProviders 外部账户提供者集合（LDAP/AD/RADIUS 容器）
func (c *Client) ExternalAccountProviders(ctx context.Context) ([]string, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/AccountService/ExternalAccountProviders")
	if err != nil {
		return nil, err
	}
	return coll.MemberIDs(), nil
}

// ---------- Registries 注册表（手册 3.69–3.73） ----------

// RegistryFile 注册表文件定位
type RegistryFile struct {
	ODataContext string   `json:"@odata.context"`
	ODataID      string   `json:"@odata.id"`
	ID           string   `json:"Id"` // 如 Base.1.12.0
	Name         string   `json:"Name"`
	Languages    []string `json:"Languages"`
	Registry     string   `json:"Registry"`
	Location     []struct {
		Language string `json:"Language"`
		Uri      string `json:"Uri"`
	} `json:"Location"`
}

// Registries 列出全部注册表（消息/属性/权限注册表）
func (c *Client) Registries(ctx context.Context) ([]*RegistryFile, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/Registries")
	if err != nil {
		return nil, err
	}
	out := make([]*RegistryFile, 0, len(coll.Members))
	for _, m := range coll.Members {
		var rf RegistryFile
		if err := c.Get(ctx, m.ODataID, &rf); err != nil {
			return nil, err
		}
		out = append(out, &rf)
	}
	return out, nil
}

// MessageRegistry 消息注册表内容（GET Registries/{id}.json）
type MessageRegistry struct {
	ODataContext    string                     `json:"@odata.context"`
	ID              string                     `json:"Id"`
	Language        string                     `json:"Language"`
	RegistryPrefix  string                     `json:"RegistryPrefix"`
	RegistryVersion string                     `json:"RegistryVersion"`
	OwningEntity    string                     `json:"OwningEntity"`
	Messages        map[string]MessageTemplate `json:"Messages"`
}

// MessageTemplate 单条消息模板（SubmitTestEvent 的 MessageId 来源）
type MessageTemplate struct {
	Description     string   `json:"Description"`
	Message         string   `json:"Message"`
	Severity        string   `json:"Severity"`
	MessageSeverity string   `json:"MessageSeverity"`
	NumberOfArgs    *int     `json:"NumberOfArgs"`
	ParamTypes      []string `json:"ParamTypes"`
	Resolution      string   `json:"Resolution"`
}

// MessageRegistry 获取消息注册表内容。registryID 如 "EventLog.1.0.0"。
func (c *Client) MessageRegistry(ctx context.Context, registryID string) (*MessageRegistry, error) {
	var mr MessageRegistry
	if err := c.Get(ctx, "/redfish/v1/Registries/"+registryID+".json", &mr); err != nil {
		return nil, err
	}
	return &mr, nil
}

// PrivilegeRegistry 权限注册表（资源×方法→所需权限映射）
type PrivilegeRegistry struct {
	ODataContext      string   `json:"@odata.context"`
	ID                string   `json:"Id"`
	PrivilegesUsed    []string `json:"PrivilegesUsed"`
	OEMPrivilegesUsed []string `json:"OEMPrivilegesUsed"`
	Mappings          []struct {
		Entity       string `json:"Entity"`
		OperationMap map[string]struct {
			Privilege []string `json:"Privilege"`
		} `json:"OperationMap"`
	} `json:"Mappings"`
}

// PrivilegeRegistry 获取权限注册表（探测某资源某方法所需权限）
func (c *Client) PrivilegeRegistry(ctx context.Context, registryID string) (*PrivilegeRegistry, error) {
	var pr PrivilegeRegistry
	if err := c.Get(ctx, "/redfish/v1/Registries/"+registryID+".json", &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// ---------- JSONSchemaFile（手册 3.52–3.53，只读） ----------

// JSONSchemaFile 本地 JSON Schema 定位
type JSONSchemaFile struct {
	ODataContext string   `json:"@odata.context"`
	ODataID      string   `json:"@odata.id"`
	ID           string   `json:"Id"`
	Name         string   `json:"Name"`
	Languages    []string `json:"Languages"`
	Schema       string   `json:"Schema"`
	Location     []struct {
		Language string `json:"Language"`
		Uri      string `json:"Uri"`
	} `json:"Location"`
}

// JSONSchemas 列出本地托管的 OEM JSON Schema
func (c *Client) JSONSchemas(ctx context.Context) ([]*JSONSchemaFile, error) {
	coll, err := c.CollectionAll(ctx, "/redfish/v1/JsonSchemas")
	if err != nil {
		return nil, err
	}
	out := make([]*JSONSchemaFile, 0, len(coll.Members))
	for _, m := range coll.Members {
		var jf JSONSchemaFile
		if err := c.Get(ctx, m.ODataID, &jf); err != nil {
			return nil, err
		}
		out = append(out, &jf)
	}
	return out, nil
}
