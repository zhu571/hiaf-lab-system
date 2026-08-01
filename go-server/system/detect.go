package system

import (
	"sort"
	"strings"
)

// Affected 表示一次变更检测的结果。
type Affected struct {
	All      bool     // 全量重建（compose/Dockerfile/.env 变更 或 --force）
	Services []string // 受影响服务（去重排序，All 时为空）
}

// Has 判断 svc 是否受影响（All 视为影响全部）。
func (a *Affected) Has(svc string) bool {
	if a.All {
		return true
	}
	for _, s := range a.Services {
		if s == svc {
			return true
		}
	}
	return false
}

// IsNone 判断是否无受影响服务。
func (a *Affected) IsNone() bool {
	return !a.All && len(a.Services) == 0
}

// String 输出与 update.sh 日志一致的表示：ALL / 空格分隔服务 / none。
func (a *Affected) String() string {
	if a.All {
		return "ALL"
	}
	if len(a.Services) == 0 {
		return "none"
	}
	return strings.Join(a.Services, " ")
}

// detectRule 有序规则：按顺序匹配，先匹配者生效。
type detectRule struct {
	all   bool     // true 表示命中即全量 ALL（短路）
	svc   []string // 命中的受影响服务
	match func(path string) bool
}

// detectRules 是 update.sh detect_services() 的逐条移植，顺序敏感：
// deploy/Dockerfile.migrate 必须先于 deploy/*，py-agent/ioc 必须先于 py-agent/*。
var detectRules = []detectRule{
	{all: true, match: func(p string) bool {
		return p == "deploy/docker-compose.yml" || p == "deploy/Dockerfile" || strings.HasPrefix(p, "deploy/.env")
	}},
	{svc: []string{"migrate"}, match: func(p string) bool { return p == "deploy/Dockerfile.migrate" }},
	{svc: []string{"server"}, match: hasDirPrefix("web-ui")},
	{svc: []string{"epics-gateway"}, match: hasDirPrefix("go-server/epics-gateway")},
	{svc: []string{"server"}, match: hasDirPrefix("go-server")},
	{svc: []string{"ioc"}, match: hasDirPrefix("py-agent/ioc")},
	{svc: []string{"py-agent", "py-agent-interpret"}, match: hasDirPrefix("py-agent")},
	{svc: []string{"migrate"}, match: hasDirPrefix("migrations")},
	{svc: []string{"server"}, match: hasDirPrefix("deploy")},
}

// hasDirPrefix 构造 shell 通配 dir/* 语义的匹配函数（不含 dir 自身）。
func hasDirPrefix(dir string) func(string) bool {
	return func(p string) bool {
		return strings.HasPrefix(p, dir+"/")
	}
}

// DetectServices 把变更路径列表映射为受影响服务。
// 与 update.sh 对齐：ALL 短路；去重排序；无匹配但含 migrations/ → 只跑 migrate；否则 none。
func DetectServices(changed []string) Affected {
	seen := make(map[string]bool)
	var svcs []string
	for _, f := range changed {
		for _, r := range detectRules {
			if !r.match(f) {
				continue
			}
			if r.all {
				return Affected{All: true}
			}
			for _, s := range r.svc {
				if !seen[s] {
					seen[s] = true
					svcs = append(svcs, s)
				}
			}
			break
		}
	}
	sort.Strings(svcs)
	if len(svcs) == 0 {
		for _, f := range changed {
			if hasDirPrefix("migrations")(f) {
				return Affected{Services: []string{"migrate"}}
			}
		}
	}
	return Affected{Services: svcs}
}
