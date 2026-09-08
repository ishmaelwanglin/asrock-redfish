package redfish

import (
	"context"
	"net/http"
	"strings"
)

// ---------- 资源 ETag 与乐观并发（Redfish 规范：写请求可要求 If-Match） ----------
// 部分厂商 BMC 强制 PATCH/PUT 携带 If-Match（缺失时返回 428 Precondition Required），
// 调用方先 ResourceETag 取当前版本，再 PatchWithETag 下发。

// ResourceETag 读取资源 ETag：优先响应头 ETag，其次响应体 @odata.etag
// （部分厂商仅放 body）。空串表示该资源不提供 ETag。
func (c *Client) ResourceETag(ctx context.Context, path string) (string, error) {
	var header string
	var body struct {
		ODataETag string `json:"@odata.etag"`
	}
	_, err := c.do(ctx, apiRequest{method: http.MethodGet, path: path, outETag: &header}, &body)
	if err != nil {
		return "", err
	}
	if header != "" {
		return strings.Trim(header, `"`), nil
	}
	return strings.Trim(body.ODataETag, `"`), nil
}

// PatchWithETag 携带 If-Match 的 PATCH（etag 来自 ResourceETag，空串时退化为裸 PATCH）。
// etag 自动补双引号（已带则不重复）。
func (c *Client) PatchWithETag(ctx context.Context, path string, body any, etag string) error {
	ifMatch := etag
	if ifMatch != "" && !strings.HasPrefix(ifMatch, `"`) {
		ifMatch = `"` + ifMatch + `"`
	}
	_, err := c.do(ctx, apiRequest{method: http.MethodPatch, path: path, body: body, ifMatch: ifMatch}, nil)
	return err
}
