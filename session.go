package pigeon

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Session 会话包装器.
type Session struct {
	Request *http.Request
	Keys    map[string]interface{}
	conn    *websocket.Conn
	output  chan *envelope
	pigeon  *Pigeon
	open    bool
	mu      *sync.RWMutex
}

// 写入信息
func (s *Session) writeMessage(message *envelope) {
	if s.closed() {
		s.pigeon.errorHandler(s, errors.New("tried to write to closed a session"))
		return
	}

	select {
	case s.output <- message:
	default:
		s.pigeon.errorHandler(s, errors.New("session message buffer is full"))
	}
}

func (s *Session) writeRaw(message *envelope) error {
	if s.closed() {
		return errors.New("tried to write to a closed session")
	}
	s.conn.SetWriteDeadline(time.Now().Add(s.pigeon.Config.WriteWait))
	return s.conn.WriteMessage(message.t, message.message)
}

// 判断会话状态
func (s *Session) closed() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return !s.open
}

// 关闭会话
func (s *Session) close() {
	if !s.closed() {
		s.mu.Lock()
		s.open = false
		s.conn.Close()
		close(s.output)
		s.mu.Unlock()
	}
}

// 向客户端发送ping信息
func (s *Session) ping() {
	s.writeRaw(&envelope{t: websocket.PingMessage, message: []byte{}})
}

// 写入信息流
func (s *Session) writePump() {
	ticker := time.NewTicker(s.pigeon.Config.PingPeriod)
	defer ticker.Stop()

loop:
	for {
		select {
		case msg, ok := <-s.output:
			if !ok {
				break loop
			}

			if err := s.writeRaw(msg); err != nil {
				s.pigeon.errorHandler(s, err)
				break loop
			}

			if msg.t == websocket.CloseMessage {
				break loop
			}

			if msg.t == websocket.TextMessage {
				s.pigeon.messageSentHandler(s, msg.message)
			}

			if msg.t == websocket.BinaryMessage {
				s.pigeon.messageSentHandlerBinary(s, msg.message)
			}
		case <-ticker.C:
			s.ping()
		}
	}
}

// 读取信息流
func (s *Session) readPump() {
	s.conn.SetReadLimit(s.pigeon.Config.MaxMessageSize)
	s.conn.SetReadDeadline(time.Now().Add(s.pigeon.Config.PongWait))

	s.conn.SetPongHandler(func(string) error {
		s.conn.SetReadDeadline(time.Now().Add(s.pigeon.Config.PongWait))
		s.pigeon.pongHandler(s)
		return nil
	})

	for {
		t, message, err := s.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure,
				websocket.CloseServiceRestart) {
				s.pigeon.errorHandler(s, err)
			}
			break
		}
		if t == websocket.TextMessage {
			s.pigeon.messageHandler(s, message)
		}
		if t == websocket.BinaryMessage {
			s.pigeon.messageHandlerBinary(s, message)
		}
	}
}

// 向会话写入普通文本信息.
func (s *Session) Write(msg []byte) error {
	if s.closed() {
		return errors.New("session is closed")
	}
	s.writeMessage(&envelope{t: websocket.TextMessage, message: msg})
	return nil
}

// WriteBinary 向会话写入二进制信息.
func (s *Session) WriteBinary(msg []byte) error {
	if s.closed() {
		return errors.New("session is closed")
	}
	s.writeMessage(&envelope{t: websocket.BinaryMessage, message: msg})
	return nil
}

// Close 关闭会话.
func (s *Session) Close() error {
	return s.CloseWithMsg([]byte{})
}

// CloseWithMsg 关闭会话时写入的信息.
func (s *Session) CloseWithMsg(msg []byte) error {
	if s.closed() {
		return errors.New("session is already closed")
	}
	s.writeMessage(&envelope{t: websocket.CloseMessage, message: msg})
	return nil
}

// Set key/value
func (s *Session) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Keys == nil {
		s.Keys = make(map[string]interface{})
	}
	s.Keys[key] = value
}

// Get 获取指定key的value
func (s *Session) Get(key string) (value interface{}, exists bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Keys != nil {
		value, exists = s.Keys[key]
	}
	return
}

// MustGet 必须具备某个key的value.
func (s *Session) MustGet(key string) interface{} {
	if value, exists := s.Get(key); exists {
		return value
	}
	panic("Key \"" + key + "\" does not exist")
}

// IsClosed 判断会话是状态
func (s *Session) IsClosed() bool {
	return s.closed()
}
