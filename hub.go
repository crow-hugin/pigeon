package pigeon

import (
	"sync"
)

type hub struct {
	sessions   map[*Session]bool
	broadcast  chan *envelope
	register   chan *Session
	unregister chan *Session
	exit       chan *envelope
	open       bool
	mu         *sync.RWMutex
}

func newHub() *hub {
	return &hub{
		sessions:   make(map[*Session]bool),
		broadcast:  make(chan *envelope),
		register:   make(chan *Session),
		unregister: make(chan *Session),
		exit:       make(chan *envelope),
		open:       true,
		mu:         &sync.RWMutex{},
	}
}

func (h *hub) run() {
loop:
	for {
		select {
		case s := <-h.register: // 注册会话
			h.mu.Lock()
			h.sessions[s] = true
			h.mu.Unlock()
		case s := <-h.unregister: // 注销会话
			if _, ok := h.sessions[s]; ok {
				h.mu.Lock()
				delete(h.sessions, s)
				h.mu.Unlock()
			}
		case m := <-h.broadcast: // 广播消息
			h.mu.RLock()
			for s := range h.sessions {
				if m.filter != nil {
					if m.filter(s) {
						s.writeMessage(m)
					}
				} else {
					s.writeMessage(m)
				}
			}
			h.mu.RUnlock()
		case m := <-h.exit: // 退出
			h.mu.Lock()
			for s := range h.sessions {
				s.CloseWithMsg(m.message)
				delete(h.sessions, s)
			}
			h.open = false
			h.mu.Unlock()
			break loop
		}
	}
}

// 关闭HUB
func (h *hub) closed() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return !h.open
}

// 获取会话数量
func (h *hub) len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

func (h *hub) filterSession(fn func(*Session) bool) *Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.sessions {
		if fn(s) {
			return s
		}
	}
	return nil
}
