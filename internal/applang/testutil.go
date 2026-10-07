package applang

// testingT 是 *testing.T / *testing.B 的最小接口：用接口而非直接依赖 testing 包，
// 避免生产包 applang 被打进 testing 及其注册的 -test.* 标志。
type testingT interface {
	Helper()
	Cleanup(func())
}

// SetForTest 在测试期间把语言设为 lang，并在测试结束（含子测试）时还原为进入时的
// 原值——而非固定 en，这样嵌套/跨包测试不会把语言态改坏。供本包及跨包测试共用。
func SetForTest(t testingT, lang string) {
	t.Helper()
	mu.RLock()
	prev := current
	mu.RUnlock()
	Set(lang)
	t.Cleanup(func() { Set(prev) })
}
