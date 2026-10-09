package providers

const codeBuddyID = "codebuddy"

// CodeBuddy 对应腾讯云 CodeBuddy Code CLI。会话与 Generic 已实测模板同构，
// 解析委托 Generic，避免两套字段路径。
type CodeBuddy struct{}

func (CodeBuddy) ID() string          { return codeBuddyID }
func (CodeBuddy) DisplayName() string { return "CodeBuddy" }

func (CodeBuddy) wrap(home string) Generic {
	return Generic{Spec: codeBuddySpec(), Home: home}
}

func codeBuddySpec() GenericSpec {
	var s GenericSpec
	s.ID = codeBuddyID
	s.Name = "CodeBuddy"
	s.Detect.Command = "codebuddy"
	s.Detect.Dirs = []string{"~/.codebuddy"}
	s.Sessions.Glob = "~/.codebuddy/projects/*/*.jsonl"
	s.Sessions.Format = "jsonl"
	s.Fields.CWD = "cwd"
	s.Fields.ID = "sessionId"
	s.Fields.Timestamp = "timestamp"
	s.Fields.Title = "summary"
	s.Fields.TitleFallbacks = []string{"aiTitle"}
	s.Resume.Args = []string{"--resume", "{id}"}
	s.Verified = true
	return s
}

func (CodeBuddy) DetectSpec(home string) DetectSpec {
	return DetectSpec{
		BinName:     "codebuddy",
		AltBinNames: []string{"cbc"},
		ConfigDirs:  []string{"~/.codebuddy"},
	}
}

func (c CodeBuddy) SessionRoots(home string) []string {
	return c.wrap(home).SessionRoots(home)
}

func (c CodeBuddy) SessionFilePattern() string {
	return c.wrap("").SessionFilePattern()
}

func (c CodeBuddy) MatchSessionRel(rel string) bool {
	return c.wrap("").MatchSessionRel(rel)
}

func (c CodeBuddy) ParseSession(path string, head []byte) (*Session, error) {
	return c.wrap("").ParseSession(path, head)
}

func (c CodeBuddy) NewSessionCmd(ws, bin string) Launch {
	return c.wrap("").NewSessionCmd(ws, bin)
}

func (c CodeBuddy) ResumeCmd(s Session, bin string) Launch {
	return c.wrap("").ResumeCmd(s, bin)
}

// RemoteNewArgs / RemoteResumeArgs：CodeBuddy 无官方远程协议，ssh 远端执行 CLI。
func (CodeBuddy) RemoteNewArgs(string) []string { return nil }
func (CodeBuddy) RemoteResumeArgs(s Session) []string {
	return []string{"--resume", s.ID}
}

// ACPAdapter 声明 CodeBuddy 的 ACP 入口：CLI 自带 --acp（stdio ndJsonStream），
// 适配器与 CLI 同二进制，故无 npx 兜底。
func (CodeBuddy) ACPAdapter() ACPAdapter {
	return ACPAdapter{
		BinNames:  []string{"codebuddy", "cbc"},
		ExtraArgs: []string{"--acp"},
	}
}

func (CodeBuddy) InstallRecipe() InstallRecipe {
	r := npmInstall("@tencent-ai/codebuddy-code")
	r.PurgeDirs = []string{"~/.codebuddy"}
	return r
}
