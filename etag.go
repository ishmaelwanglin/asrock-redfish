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

// PatchWithWildcard 以裸 If-Match: * 发送 PATCH（无版本竞态）。
//
// 用途：部分固件（AMI/AST2600 等）对 PATCH 强制要求 If-Match（缺失返回 428），
// 但其 ETag 是“随秒滚动的时间戳”，先 GET 再 PATCH 的乐观锁两步之间一旦跨秒，
// 回传旧 ETag 必然 412 Precondition Failed——这类资源上乐观锁形同虚设。
// 裸 `If-Match: *` 语义为“只要资源存在即写入”，绕开版本竞态；
// 注意必须是裸 *（不能带引号，`"*"` 会被当作名为 * 的实体标签同样 412）。
func (c *Client) PatchWithWildcard(ctx context.Context, path string, body any) error {
	_, err := c.do(ctx, apiRequest{method: http.MethodPatch, path: path, body: body, ifMatch: "*"}, nil)
	return err
}
