package common

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/raft-kv-store/raftpb"
	"github.com/subchen/go-trylock/v2"
)

type Value struct {
	V  interface{}
	mu trylock.TryLocker
	temp bool
}

func NewValue(v interface{}) *Value {
	return &Value{
		V:  v,
		mu: trylock.New(),
	}
}

func TempNewValue(v interface{}) *Value {
	return &Value{
		V:  v,
		mu: trylock.New(),
		temp: true,
	}
}

type Cmap struct {
	Map     map[string]*Value
	mu      trylock.TryLocker
	timeout time.Duration
}

func NewCmap(t time.Duration) *Cmap {
	return &Cmap{
		Map:     make(map[string]*Value),
		mu:      trylock.New(),
		timeout: t,
	}
}

func NewCmapFromMap(m map[string]interface{}, t time.Duration) *Cmap {
	res := &Cmap{
		Map:     make(map[string]*Value),
		mu:      trylock.New(),
		timeout: t,
	}
	for k, v := range m {
		res.Map[k] = NewValue(v)
	}
	return res
}

func (c *Cmap) Snapshot() map[string]interface{} {
	res := make(map[string]interface{})
	c.mu.RLock()
	defer c.mu.RLock()
	for k, v := range c.Map {
		v.mu.RLock()
		res[k] = v.V
		v.mu.RUnlock()
	}
	return res
}

func (c *Cmap) Get(k string) (val interface{}, ok bool, err error) {
	if global := c.mu.RTryLockTimeout(c.timeout); !global {
		return val, ok, errors.New("map is locked globally")
	}
	value, ok := c.Map[k]
	if !ok {
		c.mu.RUnlock() // unlock globally asap
		return val, ok, nil
	} else if local := value.mu.RTryLockTimeout(c.timeout); !local {
		c.mu.RUnlock() // unlock globally asap
		return val, ok, fmt.Errorf("map is locked on Key=%s", k)
	}
	c.mu.RUnlock()
	defer value.mu.RUnlock()
	return value.V, ok, nil
}



func (c *Cmap) benchmarkSet(k string, v, v0 interface{}, t time.Duration) error {
	if global := c.mu.TryLockTimeout(c.timeout); !global {
		return errors.New("map is locked globally")
	}
	value, ok := c.Map[k]
	if !ok {
		c.Map[k] = NewValue(v)
		c.mu.Unlock() // unlock globally asap
		return nil
	} else if local := value.mu.TryLockTimeout(c.timeout); !local {
		c.mu.Unlock() // unlock globally asap
		return fmt.Errorf("map is locked on Key=%s", k)
	}
	c.mu.Unlock()
	defer value.mu.Unlock()
	time.Sleep(t)
	if v0 != nil && value.V != v0 {
		return fmt.Errorf("condition not satisfied on Key=%s", k)
	}
	value.V = v
	return nil
}

func (c *Cmap) Set(k string, v interface{}) error {
	return c.benchmarkSet(k, v, nil,0)
}

func (c *Cmap) SetCond(k string, v, v0 interface{}) error {
	return c.benchmarkSet(k, v, v0,0)
}

func (c *Cmap) Del(k string) error {
	if global := c.mu.TryLockTimeout(c.timeout); !global {
		return errors.New("map is locked globally")
	}
	value, ok := c.Map[k]
	if !ok {
		c.mu.Unlock() // unlock globally asap
		return nil
	} else if local := value.mu.TryLockTimeout(c.timeout); !local { // Not to del if the key is locked by other op
		c.mu.Unlock() // unlock globally asap
		return fmt.Errorf("map is locked on Key=%s", k)
	}
	delete(c.Map, k)
	c.mu.Unlock()
	return nil
}

func (c *Cmap) TryLocks(ops []*raftpb.Command) error {
	if len(ops) == 0 {
		return errors.New("no key given")
	}
	if global := c.mu.TryLockTimeout(c.timeout); !global {
		return errors.New("map is locked globally")
	}
	// locked is used to revert lock if any trylock fails
	var locked []*Value
	var revert, cond bool
	// tmpMap is the local temp map for new value initialization
	tmpMap := make(map[string]*Value)
	for _, op := range ops {
		k := op.Key
		value, ok := c.Map[k]
		if !ok {
			value = TempNewValue(nil)
			tmpMap[k] = value
		}
		// trylock on each value including new init
		if local := value.mu.TryLockTimeout(c.timeout); !local {
			revert = true
			break
		} else {
			locked = append(locked, value)
			// revert all locks if condition fails
			if op.Method == SET && op.Cond != nil && op.Cond.Value != value.V {
				revert = true
				cond = true
				break
			}
		}
	}
	// Link tmpMap to c.Map if no failure
	if len(tmpMap) > 0 && !revert {
		for k, v := range tmpMap {
			c.Map[k] = v
		}
	}
	c.mu.Unlock()
	// Revert lock if failure
	if revert {
		for _, value := range locked {
			value.mu.Unlock()
		}
		if cond {
			return errors.New("set condition fails")
		}
		return errors.New("map is locked locally")
	}
	return nil
}

func (c *Cmap) WriteWithLocks(ops []*raftpb.Command) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, op := range ops {
		switch op.Method {
		case SET:
			val := c.Map[op.Key]
			val.V = op.Value
			val.temp = false
			val.mu.Unlock()
		case DEL:
			delete(c.Map, op.Key)
		default:
			log.Fatalf("Unknown op: %s", op.Method)
		}
	}
}

func (c *Cmap) AbortWithLocks(ops []*raftpb.Command) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, op := range ops {
		val := c.Map[op.Key]
		if val.temp {
			delete(c.Map, op.Key)
		} else {
			val.mu.Unlock()
		}
	}
}

type naiveMap struct {
	Map map[string]interface{}
	sync.RWMutex
	timeout time.Duration
}

func NewNaiveMap(t time.Duration) *naiveMap {
	return &naiveMap{
		Map:     make(map[string]interface{}),
		timeout: t,
	}
}

func (c *naiveMap) Get(k string) (val interface{}, ok bool, err error) {
	c.RLock()
	defer c.RUnlock()
	val, ok = c.Map[k]
	return val, ok, nil
}

func (c *naiveMap) benchmarkSet(k string, v, _ interface{}, t time.Duration) error {
	c.Lock()
	defer c.Unlock()
	time.Sleep(t)
	c.Map[k] = v
	return nil
}

func (c *naiveMap) Set(k string, v interface{}) error {
	return c.benchmarkSet(k, v, nil, 0)
}

type ConcurrentMap interface {
	Get(string) (val interface{}, ok bool, err error)
	Set(string, interface{}) error
	benchmarkSet(string, interface{}, interface{}, time.Duration) error
}
