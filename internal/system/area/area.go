// Package area 提供管理端和应用端的中国地区树，以及 IP 到地区名的查询。
// 数据与算法对齐冻结 yudao-cloud-mini 的 AreaUtils / IPUtils：
// area.csv 与 ip2region.xdb 来自同一提交的 starter-biz-ip 资源。
package area

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"sync"

	_ "embed"

	"github.com/weilhuang/yudao-cloud-go/internal/system/area/xdb"
)

const (
	idGlobal = 0
	idChina  = 1
	// AreaTypeEnum 只有国家、省份、城市、地区四层。Java format 最多向上走这么多层。
	formatSteps = 4
)

//go:embed data/area.csv
var areaCSV []byte

//go:embed data/ip2region.xdb
var ipData []byte

// Node 对齐 AreaNodeRespVO / AppAreaNodeRespVO：id、name、children。
// 叶子的 children 是空数组，不是 null，和 Java 的 ArrayList 一致。
type Node struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Children []Node `json:"children"`
}

type record struct {
	id     int
	name   string
	parent int
	kids   []int
}

var (
	loadOnce sync.Once
	tree     []Node
	nodes    map[int]*record
	searcher *xdb.Searcher
	searchMu sync.Mutex
	loadErr  error
)

func init() {
	if err := load(); err != nil {
		// 与 Java 静态初始化失败即拒绝启动相同，避免带坏数据对外返回空树。
		panic(err)
	}
}

func load() error {
	loadOnce.Do(func() {
		nodes, loadErr = parseAreas(areaCSV)
		if loadErr != nil {
			return
		}
		china := nodes[idChina]
		if china == nil {
			loadErr = fmt.Errorf("地区数据缺少中国节点")
			return
		}
		tree = make([]Node, 0, len(china.kids))
		for _, id := range china.kids {
			tree = append(tree, buildNode(id))
		}
		searcher, loadErr = xdb.NewWithBuffer(ipData)
	})
	return loadErr
}

func parseAreas(raw []byte) (map[int]*record, error) {
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.FieldsPerRecord = 4
	rows, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("读取地区 CSV: %w", err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("地区 CSV 为空")
	}
	items := map[int]*record{
		idGlobal: {id: idGlobal, name: "全球"},
	}
	for _, row := range rows[1:] {
		id, err := strconv.Atoi(strings.TrimSpace(row[0]))
		if err != nil {
			return nil, fmt.Errorf("地区编号 %q: %w", row[0], err)
		}
		parent, err := strconv.Atoi(strings.TrimSpace(row[3]))
		if err != nil {
			return nil, fmt.Errorf("地区 %d 的父编号: %w", id, err)
		}
		items[id] = &record{id: id, name: row[1], parent: parent}
	}
	for _, row := range rows[1:] {
		id, _ := strconv.Atoi(strings.TrimSpace(row[0]))
		item := items[id]
		parent := items[item.parent]
		if parent == nil {
			return nil, fmt.Errorf("地区 %d 的父节点 %d 不存在", id, item.parent)
		}
		if parent == item {
			return nil, fmt.Errorf("地区 %s 的父子节点相同", item.name)
		}
		parent.kids = append(parent.kids, id)
	}
	return items, nil
}

func buildNode(id int) Node {
	item := nodes[id]
	node := Node{ID: item.id, Name: item.name, Children: make([]Node, 0, len(item.kids))}
	for _, child := range item.kids {
		node.Children = append(node.Children, buildNode(child))
	}
	return node
}

// Tree 返回中国的直接子节点，不包含“中国”本身。顺序与 CSV 中挂到父节点的顺序一致。
func Tree() []Node {
	return tree
}

// Format 对齐 AreaUtils.format：自下而上用空格连接，遇到全球或中国则停止且不显示中国。
func Format(id int) (string, bool) {
	item := nodes[id]
	if item == nil {
		return "", false
	}
	var names []string
	for step := 0; step < formatSteps && item != nil; step++ {
		names = append([]string{item.name}, names...)
		parent := nodes[item.parent]
		if item.parent == idGlobal || item.id == idGlobal || parent == nil || parent.id == idGlobal || parent.id == idChina {
			break
		}
		item = parent
	}
	return strings.Join(names, " "), true
}

// NameByIP 对齐 AreaController.getAreaByIp。库中没有对应地区时返回“未知”。
// IP 格式错误返回 error，由接口写成系统异常，不把检索库内部信息回给调用方。
func NameByIP(ip string) (string, error) {
	searchMu.Lock()
	region, err := searcher.SearchByStr(strings.TrimSpace(ip))
	searchMu.Unlock()
	if err != nil {
		return "", err
	}
	id, err := strconv.Atoi(strings.TrimSpace(region))
	if err != nil {
		return "", fmt.Errorf("地区编号 %q", region)
	}
	name, ok := Format(id)
	if !ok {
		return "未知", nil
	}
	return name, nil
}
