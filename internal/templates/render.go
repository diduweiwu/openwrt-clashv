package templates

// render.go：把转换后的节点注入模板——
//  1. 整段替换 proxies 占位段（"proxies: ~"、空 "proxies:" 或既有缩进块），
//     生成 Clash 常见的两空格缩进列表格式（"  - name" / 四空格续行）；
//  2. 逐行替换 __ALL_PROXIES__ 占位符：独立成行的（带横杠）按原行缩进展开成
//     每节点一行，行内其他写法（如 flow 列表）按逗号串联替换；
//  3. 注入完成后整体回读校验 YAML 合法，并确认没有残留占位符。

import (
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"clashv/internal/subconv"
)

const placeholder = "__ALL_PROXIES__"

// Render 用 nodes 注入模板并返回最终配置文本。nodes 为空报错——没有节点
// 的模板渲染结果无法作为订阅使用。
func Render(templateContent string, nodes []subconv.Node) (string, error) {
	if len(nodes) == 0 {
		return "", errors.New("没有可注入的节点")
	}
	proxiesBlock, err := marshalProxies(nodes)
	if err != nil {
		return "", fmt.Errorf("节点编码失败: %w", err)
	}

	lines := strings.Split(strings.ReplaceAll(templateContent, "\r\n", "\n"), "\n")

	// 1. 替换 proxies 占位段
	start, end, state := findProxiesSection(templateContent)
	switch state {
	case proxiesMissing:
		return "", errors.New(`模板缺少 proxies 占位段（如 "proxies: ~"）`)
	case proxiesInline:
		return "", errors.New(`模板的 proxies: 后带了实际内容，不是占位写法；请留空或写成 "proxies: ~"`)
	}
	newLines := make([]string, 0, len(lines)+len(proxiesBlock))
	newLines = append(newLines, lines[:start]...)
	newLines = append(newLines, proxiesBlock...)
	newLines = append(newLines, lines[end:]...)
	lines = newLines

	// 2. 替换 __ALL_PROXIES__ 占位符（构建新切片，避免边遍历边改）
	names := make([]string, len(nodes))
	for j, n := range nodes {
		names[j] = quoteYAML(n.Name)
	}
	out := make([]string, 0, len(lines)+len(nodes)*2)
	for _, l := range lines {
		if !strings.Contains(l, placeholder) {
			out = append(out, l)
			continue
		}
		if indent, ok := matchListLine(l); ok {
			// 独立列表项：保持原缩进，每节点一行横杠列表
			for _, name := range names {
				out = append(out, indent+"- "+name)
			}
			continue
		}
		// 行内其他写法：按逗号串联（保持所在行上下文不动）
		out = append(out, strings.ReplaceAll(l, placeholder, strings.Join(names, ", ")))
	}
	lines = out

	// 3. 校验：无残留占位符 + YAML 合法 + proxies 非空列表
	for i, l := range lines {
		if strings.Contains(l, placeholder) {
			return "", fmt.Errorf("模板第 %d 行的 %s 写法无法识别，请改成独立的一行「- %s」", i+1, placeholder, placeholder)
		}
	}
	text := strings.Join(lines, "\n")
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(text), &doc); err != nil {
		return "", fmt.Errorf("注入后的配置不是合法的 YAML: %w", err)
	}
	ps, ok := doc["proxies"].([]any)
	if !ok || len(ps) == 0 {
		return "", errors.New("注入后 proxies 为空，请检查模板写法")
	}
	return text, nil
}

// marshalProxies 把节点列表编码为模板中的 proxies 段（键名行 + 横杠列表）：
//
//	proxies:
//	  - name: x
//	    type: ss
func marshalProxies(nodes []subconv.Node) ([]string, error) {
	out := []string{"proxies:"}
	for _, n := range nodes {
		body, err := subconv.MarshalNode(n)
		if err != nil {
			return nil, err
		}
		ls := strings.Split(body, "\n")
		out = append(out, "  - "+ls[0])
		for _, l := range ls[1:] {
			out = append(out, "    "+l)
		}
	}
	return out, nil
}

// findProxiesSection 在模板里定位 proxies 占位段：返回 [key 行下标, 段落后
// 第一行下标)。占位段指「proxies: ~ / null / [] / 空值」或其后跟着缩进块
// （整段一起替换）。
func findProxiesSection(content string) (int, int, int) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	for i, l := range lines {
		rest, ok := proxiesKeyRest(l)
		if !ok {
			continue
		}
		// 内联占位值：~ / null / [] / 空 / 纯注释
		if isNullValue(rest) {
			return i, consumeIndentedBlock(lines, i+1), proxiesPlaceholder
		}
		return 0, 0, proxiesInline // proxies: 后面是实际内容（如内联列表），不是占位写法
	}
	return 0, 0, proxiesMissing
}

// proxies 段的定位状态。
const (
	proxiesMissing    = iota // 模板里没有 proxies 键行
	proxiesPlaceholder    // 找到占位写法，start/end 有效
	proxiesInline         // proxies: 带了实际内容，不是占位写法
)

// proxiesKeyRest 判断一行是否为顶层 proxies 键行，返回冒号后的剩余文本。
func proxiesKeyRest(l string) (string, bool) {
	if !strings.HasPrefix(l, "proxies:") {
		return "", false
	}
	return l[len("proxies:"):], true
}

func isNullValue(rest string) bool {
	t := strings.TrimSpace(rest)
	switch t {
	case "", "~", "null", "[]", "Null", "NULL":
		return true
	}
	return strings.HasPrefix(t, "#")
}

// consumeIndentedBlock 吸收 key 行后面连续的缩进行（含中间空行，但空行后
// 若回到顶格则停止）。返回段落后的第一行下标。
func consumeIndentedBlock(lines []string, j int) int {
	for j < len(lines) {
		l := lines[j]
		if strings.TrimSpace(l) == "" {
			// 空行：向后看，下一个非空行仍是缩进行才算块内
			k := j + 1
			for k < len(lines) && strings.TrimSpace(lines[k]) == "" {
				k++
			}
			if k < len(lines) && isIndented(lines[k]) {
				j = k
				continue
			}
			break
		}
		if isIndented(l) {
			j++
			continue
		}
		break
	}
	return j
}

func isIndented(l string) bool {
	return l[0] == ' ' || l[0] == '\t'
}

// matchListLine 识别「独立一行的 - __ALL_PROXIES__」，返回横杠前缩进。
func matchListLine(l string) (string, bool) {
	t := strings.TrimLeft(l, " \t")
	if !strings.HasPrefix(t, "-") {
		return "", false
	}
	item := strings.TrimSpace(t[1:])
	if item != placeholder && !strings.HasPrefix(item, placeholder+" ") {
		return "", false
	}
	return l[:len(l)-len(t)], true
}

// quoteYAML 把字符串编码成合法 YAML 标量（需要时自动加引号），去掉末尾换行。
func quoteYAML(s string) string {
	b, err := yaml.Marshal(s)
	if err != nil {
		return s
	}
	return strings.TrimRight(string(b), "\n")
}
