package demo

import "testing"

// TestResourceSpecsAreRealistic 校验演示资源规格(纯函数,不依赖 DB):
// 分类合法且齐全、存在置顶、浏览计数多样、时间线分散。见 docs/specs/dev-demo-data.md §2/§4。
func TestResourceSpecsAreRealistic(t *testing.T) {
	builtin := map[string]bool{"文献": true, "数据集": true, "代码": true, "工具": true, "模板": true, "其他": true}

	specs := linkResourceSpecs()
	specs = append(specs, fileResourceSpecs(map[string]int64{})...)
	for _, def := range demoImageDefs() {
		specs = append(specs, imageResourceSpec(def))
	}
	if len(specs) < 12 {
		t.Fatalf("资源规格数量应 ≥12,实际 %d", len(specs))
	}

	seenKey := map[string]bool{}
	cats := map[string]int{}
	views := map[int64]bool{}
	var pinned, maxAge, minAge int
	minAge = 1 << 30
	for _, sp := range specs {
		if sp.key == "" {
			t.Errorf("资源规格缺少 key: %q", sp.title)
		}
		if seenKey[sp.key] {
			t.Errorf("资源规格 key 重复: %q", sp.key)
		}
		seenKey[sp.key] = true
		if !builtin[sp.category] {
			t.Errorf("资源 %q 分类非法: %q", sp.title, sp.category)
		}
		cats[sp.category]++
		views[sp.views] = true
		if sp.pinned {
			pinned++
		}
		if sp.ageHours > maxAge {
			maxAge = sp.ageHours
		}
		if sp.ageHours < minAge {
			minAge = sp.ageHours
		}
	}

	if len(cats) < 6 {
		t.Errorf("资源应覆盖全部 6 个内置分类,实际 %d: %v", len(cats), cats)
	}
	if pinned < 2 {
		t.Errorf("置顶资源应 ≥2,实际 %d", pinned)
	}
	if len(views) < 5 {
		t.Errorf("浏览计数应有多样取值,实际 %d 种", len(views))
	}
	if maxAge-minAge < 24*5 {
		t.Errorf("资源时间线跨度应 ≥5 天,实际 %d 小时", maxAge-minAge)
	}
}
