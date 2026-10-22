package redfish

import (
	"context"
	"testing"
)

// ---------- 显式写入引导顺序（ApplySystemBootOrder）与旧接口的稳健化 ----------

// lastPatchPayload 返回最后一次 PATCH 的 Boot 段
func lastPatchPayload(t *testing.T, f *fakeBootBMC) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.patchPayloads) == 0 {
		t.Fatalf("没有任何 PATCH 记录")
	}
	boot, _ := f.patchPayloads[len(f.patchPayloads)-1]["Boot"].(map[string]any)
	if boot == nil {
		t.Fatalf("最后一次 PATCH 没有 Boot 段: %v", f.patchPayloads[len(f.patchPayloads)-1])
	}
	return boot
}

func TestApplySystemBootOrderWritesSettingsPath(t *testing.T) {
	f := defaultFakeBoot()
	c := newFakeBootServer(t, f)
	ctx := context.Background()

	want := []string{"Boot000E", "Boot0003", "Boot0002"}
	st, err := c.ApplySystemBootOrder(ctx, "", want)
	if err != nil {
		t.Fatalf("ApplySystemBootOrder(): %v", err)
	}
	if !st.Written || !st.NeedsRestart {
		t.Fatalf("状态不符: %+v", st)
	}
	if st.Entry != "Boot000E" {
		t.Fatalf("Entry=%q，期望 Boot000E", st.Entry)
	}
	if st.EffectiveFirst != "Boot0002" || st.FutureFirst != "" {
		t.Fatalf("诊断字段不符: %+v", st)
	}
	f.mu.Lock()
	got := append([]string{}, f.sdBootOrder...)
	main := append([]string{}, f.bootOrder...)
	patches := append([]string{}, f.patches...)
	f.mu.Unlock()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("SD 未来状态顺序=%v，期望 %v", got, want)
	}
	if main[0] != "Boot0002" {
		t.Fatalf("主资源生效顺序被改动=%v（应只写未来状态）", main)
	}
	if len(patches) == 0 || patches[0] != "/redfish/v1/Systems/Self/SD" {
		t.Fatalf("应写 Settings 路径，实际=%v", patches)
	}
	// SD 写必须带三件套（AST2600 缺一 400）
	boot := lastPatchPayload(t, f)
	for _, k := range []string{"BootSourceOverrideTarget", "BootSourceOverrideEnabled", "BootSourceOverrideMode"} {
		if _, ok := boot[k]; !ok {
			t.Fatalf("SD 写缺字段 %s: %v", k, boot)
		}
	}
}

func TestApplySystemBootOrderRejectsEmptyOrder(t *testing.T) {
	f := defaultFakeBoot()
	c := newFakeBootServer(t, f)
	if _, err := c.ApplySystemBootOrder(context.Background(), "", nil); err == nil {
		t.Fatalf("空顺序应报错")
	}
}

func TestApplySystemBootOrderRejectsEmptyElement(t *testing.T) {
	f := defaultFakeBoot()
	c := newFakeBootServer(t, f)
	if _, err := c.ApplySystemBootOrder(context.Background(), "", []string{"Boot000E", " "}); err == nil {
		t.Fatalf("含空元素的顺序应报错")
	}
}

// 固件 2xx 却不落盘时必须报错（回读是唯一可信判据）
func TestApplySystemBootOrderDetectsFirmwareDrop(t *testing.T) {
	f := defaultFakeBoot()
	f.silentDropBootOrder = true
	c := newFakeBootServer(t, f)
	if _, err := c.ApplySystemBootOrder(context.Background(), "", []string{"Boot000E", "Boot0003"}); err == nil {
		t.Fatalf("固件未落盘时应报错")
	}
}

func TestSetSystemBootOrderUsesSettingsPathWithTriple(t *testing.T) {
	f := defaultFakeBoot()
	c := newFakeBootServer(t, f)
	ctx := context.Background()

	if err := c.SetSystemBootOrder(ctx, "", []string{"Boot000E", "Boot0003"}); err != nil {
		t.Fatalf("SetSystemBootOrder(): %v", err)
	}
	f.mu.Lock()
	patches := append([]string{}, f.patches...)
	sd := append([]string{}, f.sdBootOrder...)
	f.mu.Unlock()
	if len(patches) == 0 || patches[0] != "/redfish/v1/Systems/Self/SD" {
		t.Fatalf("应优先写 Settings 路径，实际=%v", patches)
	}
	if len(sd) == 0 || sd[0] != "Boot000E" {
		t.Fatalf("SD 顺序未落盘: %v", sd)
	}
	boot := lastPatchPayload(t, f)
	if boot["BootSourceOverrideMode"] == nil {
		t.Fatalf("SD 写缺引导三件套: %v", boot)
	}
}

func TestSetSystemBootOrderFallsBackToMainResource(t *testing.T) {
	f := defaultFakeBoot()
	f.sdPatchFail = true
	c := newFakeBootServer(t, f)
	ctx := context.Background()

	if err := c.SetSystemBootOrder(ctx, "", []string{"Boot000E", "Boot0003"}); err != nil {
		t.Fatalf("SetSystemBootOrder(): %v", err)
	}
	f.mu.Lock()
	main := append([]string{}, f.bootOrder...)
	f.mu.Unlock()
	if len(main) == 0 || main[0] != "Boot000E" {
		t.Fatalf("回退写主资源未生效: %v", main)
	}
}

func TestSetSystemBootOrderRejectsEmptyOrder(t *testing.T) {
	f := defaultFakeBoot()
	c := newFakeBootServer(t, f)
	if err := c.SetSystemBootOrder(context.Background(), "", []string{}); err == nil {
		t.Fatalf("空顺序应报错")
	}
}

func TestSetSystemBootOrderSDWritesTripleAndVerifies(t *testing.T) {
	f := defaultFakeBoot()
	c := newFakeBootServer(t, f)
	ctx := context.Background()

	if err := c.SetSystemBootOrderSD(ctx, "", []string{"Boot000E", "Boot0003"}); err != nil {
		t.Fatalf("SetSystemBootOrderSD(): %v", err)
	}
	boot := lastPatchPayload(t, f)
	for _, k := range []string{"BootSourceOverrideTarget", "BootSourceOverrideEnabled", "BootSourceOverrideMode"} {
		if _, ok := boot[k]; !ok {
			t.Fatalf("SD 写缺字段 %s: %v", k, boot)
		}
	}
	f.mu.Lock()
	sd := append([]string{}, f.sdBootOrder...)
	f.mu.Unlock()
	if len(sd) == 0 || sd[0] != "Boot000E" {
		t.Fatalf("SD 顺序未落盘: %v", sd)
	}
}

func TestSetSystemBootOrderSDRequiresSettingsResource(t *testing.T) {
	f := defaultFakeBoot()
	f.noSettings = true
	c := newFakeBootServer(t, f)
	if err := c.SetSystemBootOrderSD(context.Background(), "", []string{"Boot000E"}); err == nil {
		t.Fatalf("无 Settings 资源时应明确报错（不静默改写主资源）")
	}
}

func TestSetSystemBootOrderSDRejectsEmptyOrder(t *testing.T) {
	f := defaultFakeBoot()
	c := newFakeBootServer(t, f)
	if err := c.SetSystemBootOrderSD(context.Background(), "", nil); err == nil {
		t.Fatalf("空顺序应报错")
	}
}
