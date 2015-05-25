package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

type ToxicCollection struct {
	sync.Mutex

	noop   *ToxicWrapper
	proxy  *Proxy
	chain  []*ToxicWrapper
	toxics []*ToxicWrapper
	links  map[string]*ToxicLink
}

func NewToxicCollection(proxy *Proxy) *ToxicCollection {
	toxicOrder := []*ToxicWrapper{
		{new(SlowCloseToxic), false, -1},
		{new(LatencyToxic), false, -1},
		{new(BandwidthToxic), false, -1},
		{new(TimeoutToxic), false, -1},
	}

	collection := &ToxicCollection{
		noop:   &ToxicWrapper{new(NoopToxic), true, 0},
		proxy:  proxy,
		chain:  make([]*ToxicWrapper, 1),
		toxics: toxicOrder,
		links:  make(map[string]*ToxicLink),
	}
	collection.chain[0] = collection.noop
	return collection
}

func (c *ToxicCollection) ResetToxics() {
	c.Lock()
	defer c.Unlock()

	for index, toxic := range c.toxics {
		toxic.Enabled = false
		c.setToxic(toxic, index)
	}
}

func (c *ToxicCollection) GetToxic(name string) Toxic {
	c.Lock()
	defer c.Unlock()

	for _, toxic := range c.toxics {
		if toxic.Name() == name {
			return toxic
		}
	}
	return nil
}

func (c *ToxicCollection) GetToxicMap() map[string]Toxic {
	c.Lock()
	defer c.Unlock()

	result := make(map[string]Toxic)
	for _, toxic := range c.toxics {
		result[toxic.Name()] = toxic
	}
	return result
}

func (c *ToxicCollection) SetToxicJson(name string, data io.Reader) (Toxic, error) {
	c.Lock()
	defer c.Unlock()

	for index, toxic := range c.toxics {
		if toxic.Name() == name {
			err := json.NewDecoder(data).Decode(toxic)
			if err != nil {
				return nil, err
			}

			c.setToxic(toxic, index)
			return toxic, nil
		}
	}
	return nil, fmt.Errorf("Bad toxic type: %s", name)
}

func (c *ToxicCollection) SetToxicValue(toxic Toxic) error {
	c.Lock()
	defer c.Unlock()

	for index, toxic2 := range c.toxics {
		if toxic2.Name() == toxic.Name() {
			c.setToxic(toxic, index)
			return nil
		}
	}
	return fmt.Errorf("Bad toxic type: %v", toxic)
}

// Assumes lock has already been grabbed
func (c *ToxicCollection) setToxic(toxic Toxic, index int) {
	if !toxic.Enabled {
		c.chain[index] = c.noop
	} else {
		c.chain[index] = toxic
	}

	// Asynchronously update the toxic in each link
	group := sync.WaitGroup{}
	for _, link := range c.links {
		group.Add(1)
		go func(link *ToxicLink) {
			defer group.Done()
			link.SetToxic(c.chain[index], index)
		}(link)
	}
	group.Wait()
}

func (c *ToxicCollection) StartLink(name string, input io.Reader, output io.WriteCloser) {
	c.Lock()
	defer c.Unlock()

	link := NewToxicLink(c.proxy, c)
	link.Start(name, input, output)
	c.links[name] = link
}

func (c *ToxicCollection) RemoveLink(name string) {
	c.Lock()
	defer c.Unlock()
	delete(c.links, name)
}
