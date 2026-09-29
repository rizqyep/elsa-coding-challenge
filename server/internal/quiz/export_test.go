package quiz

// NextLua exposes the Lua state machine to tests.
var NextLua = nextSrc

// Refs reports how many holders a cached set has (0 if not cached).
func (c *Cache) Refs(id QuestionSetID) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if e, ok := c.entries[id]; ok {
		return e.refs
	}
	return 0
}
