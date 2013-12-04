package levelredis

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
)

var (
	WrongKindError   = errors.New("wrong kind error")
	BadArgumentCount = errors.New("bad argument count")
	BadArgumentType  = errors.New("bad argument type")
	MapInterfaceType = reflect.TypeOf(make(map[string]interface{}))
	miitype          = reflect.TypeOf(make(map[interface{}]interface{}))
)

const (
	dot = "."
)

// 提供面向document操作的map
type MapDocument struct {
	data map[string]interface{}
}

func NewMapDocument(data map[string]interface{}) (m *MapDocument) {
	m = &MapDocument{}
	if m.data = data; m.data == nil {
		m.data = make(map[string]interface{})
	}
	return
}

// doc_set(key, {"name":"latermoon", "$rpush":["photos", "c.jpg", "d.jpg"], "$incr":["version", 1]})
func (m *MapDocument) RichSet(input map[string]interface{}) (err error) {
	for k, v := range input {
		if !strings.HasPrefix(k, "$") {
			parent, key, _, _ := m.findElement(k, true)
			parent[key] = v
			continue
		}
		action := k[1:]
		switch action {
		case "set":
			argmap := v.(map[string]interface{})
			for field, value := range argmap {
				parent, key, _, _ := m.findElement(field, true)
				parent[key] = value
			}
		case "rpush":
			argmap := v.(map[string]interface{})
			for field, value := range argmap {
				parent, key, _, _ := m.findElement(field, true)
				m.doRpush(parent, key, value.([]interface{}))
			}
		case "inc":
			argmap := v.(map[string]interface{})
			for field, value := range argmap {
				parent, key, _, _ := m.findElement(field, true)
				err = m.doIncr(parent, key, value)
			}
		case "del":
			arglist := v.([]interface{})
			for _, field := range arglist {
				parent, key, _, exist := m.findElement(field.(string), false)
				if exist {
					delete(parent, key)
				}
			}
		default:
		}
	}
	return
}

// doc_get(key, ["name", "setting.mute", "photos.$1"])
func (m *MapDocument) RichGet(fields ...string) (result map[string]interface{}) {
	result = make(map[string]interface{})
	if len(fields) == 0 {
		for k, v := range m.data {
			result[k] = v
		}
		return
	}

	for _, field := range fields {
		dstparent := result
		srcparent := m.data
		// 逐个字段扫描copy
		pairs := strings.Split(field, dot)
		for i := 0; i < len(pairs); i++ {
			curkey := pairs[i]
			// var ok bool
			obj, ok := srcparent[curkey]
			if !ok {
				continue
			} else if reflect.TypeOf(srcparent[curkey]) != MapInterfaceType {
				// 基础类型
				dstparent[curkey] = obj
				continue
			}
			dstparent[curkey] = make(map[string]interface{})
			srcparent = srcparent[curkey].(map[string]interface{})
			dstparent = dstparent[curkey].(map[string]interface{})
		}
		key := pairs[len(pairs)-1]
		if obj, ok := srcparent[key]; ok {
			dstparent[key] = obj
		}
	}
	return
}

/**
 * 根据field路径查找元素
 * @param field 多级的field使用"."分隔
 * @return parent[key] == obj，其中 parent 目标元素父对象，必定是map[string]interface{}，key 目标元素key，obj，目标元素
 */
func (m *MapDocument) findElement(field string, createIfMissing bool) (parent map[string]interface{}, key string, obj interface{}, exist bool) {
	pairs := strings.Split(field, dot)
	parent = m.data
	for i := 0; i < len(pairs)-1; i++ {
		curkey := pairs[i]
		var ok bool
		_, ok = parent[curkey]
		// 初始化或覆盖
		if !ok || reflect.TypeOf(parent[curkey]) != MapInterfaceType {
			if createIfMissing {
				parent[curkey] = make(map[string]interface{})
			} else {
				exist = false
				return
			}
		}
		parent = parent[curkey].(map[string]interface{})
	}
	exist = true
	key = pairs[len(pairs)-1]
	obj = parent[key]
	return
}

func (m *MapDocument) doRpush(parent map[string]interface{}, key string, elems []interface{}) (err error) {
	obj := parent[key]
	if obj != nil {
		for i := 0; i < len(elems); i++ {
			parent[key] = append(parent[key].([]interface{}), elems[i])
		}
	} else {
		parent[key] = elems
	}
	return
}

func (m *MapDocument) doIncr(parent map[string]interface{}, key string, value interface{}) (err error) {
	obj := parent[key]
	if obj == nil {
		parent[key] = value
	} else {
		oldint, e1 := toInt(obj)
		incrint, e2 := toInt(value)
		if e1 != nil || e2 != nil {
			return BadArgumentType
		}
		parent[key] = oldint + incrint
	}
	return
}

func toInt(obj interface{}) (n int, err error) {
	switch obj.(type) {
	case int:
		n = obj.(int)
	case float64:
		n = int(obj.(float64))
	default:
		err = BadArgumentType
	}
	return
}

func (m *MapDocument) String() string {
	b, _ := json.Marshal(m.data)
	return string(b)
}

func (m *MapDocument) Map() map[string]interface{} {
	return m.data
}
