package center

// 客服聊天各测试共用：钩子里的异步工作在测试中同步执行，保证确定性。
func init() {
	chatAsync = func(f func()) { f() }
}
