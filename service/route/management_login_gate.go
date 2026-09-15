package route

import (
	"crypto/sha256"
	"strings"
	"sync"
	"time"
)

type loginWindow struct {
	count int
	until time.Time
}
type managementLoginGate struct {
	mu      sync.Mutex
	windows map[[32]byte]loginWindow
}

func newManagementLoginGate() *managementLoginGate {
	return &managementLoginGate{windows: make(map[[32]byte]loginWindow)}
}
func (g *managementLoginGate) allow(account string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	key := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(account))))
	w, exists := g.windows[key]
	if !exists || !now.Before(w.until) {
		if len(g.windows) >= 5000 {
			for k, v := range g.windows {
				if !now.Before(v.until) {
					delete(g.windows, k)
				}
			}
			if len(g.windows) >= 5000 && !exists {
				return false
			}
		}
		w = loginWindow{until: now.Add(5 * time.Minute)}
	}
	if w.count >= 20 {
		return false
	}
	w.count++
	g.windows[key] = w
	return true
}
