package desktop

// panelNavigation 独立记录显式入口，普通显隐广播不能覆盖用户手动选择的页签。
type panelNavigation struct {
	section  string
	target   string
	revision uint64
}

// request 只接受宿主固定目的地；重复打开同一目的地仍产生新的定位意图。
func (p *panelNavigation) request(section, target string) bool {
	valid := (section == "overview" || section == "connections") && target == "" ||
		section == "settings" && target == "notifications"
	if !valid {
		return false
	}
	p.section, p.target = section, target
	p.revision++
	return true
}
