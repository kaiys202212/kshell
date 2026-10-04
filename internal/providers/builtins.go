package providers

// Builtins 返回内置工具清单，顺序即界面展示顺序。
func Builtins() []Provider {
	return []Provider{Claude{}, Codex{}, Cursor{}, Gemini{}, Opencode{}}
}

// MergeProviders 把内置清单与 ~/.kshell/providers.yaml 里的自定义定义合并，按 ID 去重且内置优先。
// 老用户的 yaml 里可能还留着已被内置取代的条目（如 opencode），不去重会出现两个同 ID 的工具。
func MergeProviders(builtins []Provider, specs []GenericSpec, home string) []Provider {
	seen := make(map[string]bool, len(builtins)+len(specs))
	out := make([]Provider, 0, len(builtins)+len(specs))

	add := func(id string, p Provider) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, p)
	}

	for _, p := range builtins {
		add(p.ID(), p)
	}
	for _, spec := range specs {
		// 先取出 ID：id 为空时 Generic.ID() 会退化成 "generic"，与其他 provider 同口径。
		add(Generic{Spec: spec, Home: home}.ID(), Generic{Spec: spec, Home: home})
	}
	return out
}
