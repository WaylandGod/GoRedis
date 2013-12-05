package levelredis

// 基于leveldb实现的list，主要用于海量存储，比如aof、日志
// 本页面命名注意，idx都表示大于l.start的那个索引序号，而不是0开始的数组序号

import (
	"bytes"
	// "fmt"
	"github.com/latermoon/levigo"
	"strconv"
	"sync"
)

type Element struct {
	Value interface{}
}

// LevelList的特点
// 类似双向链表，右进左出，可以通过索引查找
// 海量存储，占用内存小
type LevelList struct {
	redis    *LevelRedis
	entryKey string
	// 游标控制
	start         int64
	end           int64
	mu            sync.Mutex
	inTransaction bool // 处于事务过程
}

func NewLevelList(redis *LevelRedis, entryKey string) (l *LevelList) {
	l = &LevelList{}
	l.redis = redis
	l.entryKey = entryKey
	l.start = 0
	l.end = -1
	l.initInfo()
	return
}

func (l *LevelList) Size() int {
	return 0
}

func (l *LevelList) initInfo() {
	data, err := l.redis.db.Get(l.redis.ro, l.infoKey())
	if err != nil {
		return
	}
	pairs := bytes.Split(data, []byte(","))
	if len(pairs) < 2 {
		return
	}
	l.start, _ = strconv.ParseInt(string(pairs[0]), 10, 64)
	l.end, _ = strconv.ParseInt(string(pairs[1]), 10, 64)
}

// __key:[entry key]:list =
func (l *LevelList) infoKey() []byte {
	return joinStringBytes(KEY_PREFIX, SEP_LEFT, l.entryKey, SEP_RIGHT, LIST_SUFFIX)
}

func (l *LevelList) infoValue() []byte {
	s := strconv.FormatInt(l.start, 10)
	e := strconv.FormatInt(l.end, 10)
	return []byte(s + "," + e)
}

func (l *LevelList) keyPrefix() []byte {
	return joinStringBytes(LIST_PREFIX, SEP_LEFT, l.entryKey, SEP_RIGHT)
}

// __list:[key]:idx:1005 = hello
func (l *LevelList) idxKey(idx int64) []byte {
	idxStr := strconv.FormatInt(idx, 10)
	return joinStringBytes(LIST_PREFIX, SEP_LEFT, l.entryKey, SEP_RIGHT, "idx", ":", idxStr)
}

// 打开事务后，每次push不会更新infoKey()的内容，以达到提速和事务效果
func (l *LevelList) BeginTransaction() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inTransaction {
		return
	}
	l.inTransaction = true
}

func (l *LevelList) Commit() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.inTransaction {
		return
	}
	if l.len() == 0 {
		l.redis.db.Delete(l.redis.wo, l.infoKey())
	} else {
		l.redis.db.Put(l.redis.wo, l.infoKey(), l.infoValue())
	}
	l.inTransaction = false
}

func (l *LevelList) LPush(values ...[]byte) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 左游标
	oldstart := l.start
	batch := levigo.NewWriteBatch()
	for _, value := range values {
		l.start--
		batch.Put(l.idxKey(l.start), value)
	}
	if !l.inTransaction {
		batch.Put(l.infoKey(), l.infoValue())
	}
	err = l.redis.db.Write(l.redis.wo, batch)
	if err != nil {
		// 回退
		l.start = oldstart
	}
	return
}

func (l *LevelList) RPush(values ...[]byte) (err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 右游标
	oldend := l.end
	batch := levigo.NewWriteBatch()
	for _, value := range values {
		l.end++
		batch.Put(l.idxKey(l.end), value)
	}
	if !l.inTransaction {
		batch.Put(l.infoKey(), l.infoValue())
	}
	err = l.redis.db.Write(l.redis.wo, batch)
	if err != nil {
		// 回退
		l.end = oldend
	}
	return
}

func (l *LevelList) RPop() (e *Element, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.len() == 0 {
		return nil, nil
	}
	// backup
	oldstart, oldend := l.start, l.end

	// get
	idx := l.end
	e = &Element{}
	e.Value, err = l.redis.db.Get(l.redis.ro, l.idxKey(idx))
	if err != nil {
		return nil, err
	}

	// 只剩下一个元素时，删除infoKey(0)
	shouldReset := l.len() == 1
	// 删除数据, 更新左游标
	batch := levigo.NewWriteBatch()
	batch.Delete(l.idxKey(idx))
	if shouldReset {
		l.start = 0
		l.end = -1
		batch.Delete(l.infoKey())
	} else {
		l.end--
		batch.Put(l.infoKey(), l.infoValue())
	}
	err = l.redis.db.Write(l.redis.wo, batch)
	if err != nil {
		// 回退
		l.start, l.end = oldstart, oldend
	}
	return
}

func (l *LevelList) LPop() (e *Element, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.len() == 0 {
		return nil, nil
	}
	// backup
	oldstart, oldend := l.start, l.end

	// get
	idx := l.start
	e = &Element{}
	e.Value, err = l.redis.db.Get(l.redis.ro, l.idxKey(idx))
	if err != nil {
		return nil, err
	}
	// 只剩下一个元素时，删除infoKey(0)
	shouldReset := l.len() == 1
	// 删除数据, 更新左游标
	batch := levigo.NewWriteBatch()
	batch.Delete(l.idxKey(idx))
	if shouldReset {
		l.start = 0
		l.end = -1
		batch.Delete(l.infoKey())
	} else {
		l.start++
		batch.Put(l.infoKey(), l.infoValue())
	}
	err = l.redis.db.Write(l.redis.wo, batch)
	if err != nil {
		// 回退
		l.start, l.end = oldstart, oldend
	}
	return
}

// 删除左边
func (l *LevelList) TrimLeft(count uint) (n int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.len() == 0 {
		return
	}
	oldstart, oldend := l.start, l.end
	batch := levigo.NewWriteBatch()
	for idx := oldstart; idx < (oldstart+int64(count)) && idx <= oldend; idx++ {
		batch.Delete(l.idxKey(idx))
		l.start++
	}
	shouldReset := l.len() == 0
	if shouldReset {
		l.start = 0
		l.end = -1
		batch.Delete(l.infoKey())
	} else {
		batch.Put(l.infoKey(), l.infoValue())
	}
	err := l.redis.db.Write(l.redis.wo, batch)
	if err != nil {
		// 回退
		l.start, l.end = oldstart, oldend
	}
	return
}

func (l *LevelList) Index(i int64) (e *Element, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if i < 0 || i >= l.len() {
		return nil, nil
	}
	idx := l.start + i
	e = &Element{}
	e.Value, err = l.redis.db.Get(l.redis.ro, l.idxKey(idx))
	if err != nil {
		return nil, err
	}
	return
}

func (l *LevelList) len() int64 {
	if l.end < l.start {
		return 0
	}
	return l.end - l.start + 1
}

func (l *LevelList) Len() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.len()
}

func (l *LevelList) Drop() (ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	min := l.keyPrefix()
	max := append(min, MAXBYTE)
	batch := levigo.NewWriteBatch()
	l.redis.Enumerate(min, max, IteratorForward, func(i int, key, value []byte, quit *bool) {
		batch.Delete(key)
	})
	batch.Delete(l.infoKey())
	l.redis.db.Write(l.redis.wo, batch)
	ok = true
	l.start = 0
	l.end = -1
	return
}