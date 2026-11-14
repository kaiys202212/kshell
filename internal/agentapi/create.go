package agentapi

// create.go：结构化创建与磁盘会话激活，直接转发 Backend。

// Create 新建 ACP 会话（toolID/workspace/model 语义与 desktop OpenWorkspaceACP 一致）。
func (a *API) Create(toolID, wsID, model string) (Session, error) {
	return a.b.Create(toolID, wsID, model)
}

// Open 激活一个磁盘会话（ResumeCmd 语义）。
func (a *API) Open(sessionID string) (Session, error) {
	return a.b.Open(sessionID)
}
