package pigeon

import (
	"errors"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type handleMessageFunc func(*Session, []byte)
type handleErrorFunc func(*Session, error)
type handleCloseFunc func(*Session, int, string) error
type handleSessionFunc func(*Session)
type filterFunc func(*Session) bool

// Pigeon websocket 管理器.
type Pigeon struct {
	Config                   *Config
	UpGrader                 *websocket.Upgrader
	messageHandler           handleMessageFunc
	messageHandlerBinary     handleMessageFunc
	messageSentHandler       handleMessageFunc
	messageSentHandlerBinary handleMessageFunc
	errorHandler             handleErrorFunc
	closeHandler             handleCloseFunc
	connectHandler           handleSessionFunc
	disconnectHandler        handleSessionFunc
	pongHandler              handleSessionFunc
	hub                      *hub
}

// New 新建信鸽实例.
func New(conf *Config) *Pigeon {
	upGrader := &websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin:     func(r *http.Request) bool { return true },
	}

	hub := newHub()

	go hub.run()
	if conf == nil {
		conf = defaultConfig()
	}
	return &Pigeon{
		Config:                   conf,
		UpGrader:                 upGrader,
		messageHandler:           func(*Session, []byte) {},
		messageHandlerBinary:     func(*Session, []byte) {},
		messageSentHandler:       func(*Session, []byte) {},
		messageSentHandlerBinary: func(*Session, []byte) {},
		errorHandler:             func(*Session, error) {},
		closeHandler:             nil,
		connectHandler:           func(*Session) {},
		disconnectHandler:        func(*Session) {},
		pongHandler:              func(*Session) {},
		hub:                      hub,
	}
}

// HandleConnect 会话连接时的处理方法.
func (p *Pigeon) HandleConnect(fn func(*Session)) {
	p.connectHandler = fn
}

// HandleDisconnect 会话断开时的处理方法.
func (p *Pigeon) HandleDisconnect(fn func(*Session)) {
	p.disconnectHandler = fn
}

// HandlePong 从会话中收到pong信息时的处理方法.
func (p *Pigeon) HandlePong(fn func(*Session)) {
	p.pongHandler = fn
}

// HandleMessage 收到信息时的处理方法.
func (p *Pigeon) HandleMessage(fn func(*Session, []byte)) {
	p.messageHandler = fn
}

// HandleMessageBinary 收到二进制信息的处理方法.
func (p *Pigeon) HandleMessageBinary(fn func(*Session, []byte)) {
	p.messageHandlerBinary = fn
}

// HandleSentMessage 发送信息时的处理方法.
func (p *Pigeon) HandleSentMessage(fn func(*Session, []byte)) {
	p.messageSentHandler = fn
}

// HandleSentMessageBinary 发送二进制信息的处理方法.
func (p *Pigeon) HandleSentMessageBinary(fn func(*Session, []byte)) {
	p.messageSentHandlerBinary = fn
}

// HandleError 发生错误时的处理方法.
func (p *Pigeon) HandleError(fn func(*Session, error)) {
	p.errorHandler = fn
}

// HandleClose 信鸽关闭时的处理方法
func (p *Pigeon) HandleClose(fn func(*Session, int, string) error) {
	if fn != nil {
		p.closeHandler = fn
	}
}

// HandleRequest 将http请求升级成websocket连接，并将其注册到信鸽实例进行管理.
func (p *Pigeon) HandleRequest(w http.ResponseWriter, r *http.Request) error {
	return p.HandleRequestWithKeys(w, r, nil)
}

// HandleRequestWithKeys 与HandleRequest功能相同，增加keys.
func (p *Pigeon) HandleRequestWithKeys(w http.ResponseWriter, r *http.Request, keys map[string]interface{}) error {
	if p.hub.closed() {
		return errors.New("pigeon instance is closed")
	}

	conn, err := p.UpGrader.Upgrade(w, r, nil)

	if err != nil {
		return err
	}

	session := &Session{
		Request: r,
		Keys:    keys,
		conn:    conn,
		output:  make(chan *envelope, p.Config.MessageBufferSize),
		pigeon:  p,
		open:    true,
		mu:      &sync.RWMutex{},
	}

	p.hub.register <- session

	p.connectHandler(session)

	if p.closeHandler != nil {
		session.conn.SetCloseHandler(func(code int, text string) error {
			return p.closeHandler(session, code, text)
		})
	}

	go session.writePump()

	session.readPump()

	if !p.hub.closed() {
		p.hub.unregister <- session
	}

	session.close()

	p.disconnectHandler(session)

	return nil
}

// Broadcast 广播消息.
func (p *Pigeon) Broadcast(msg []byte) error {
	if p.hub.closed() {
		return errors.New("pigeon instance is closed")
	}

	message := &envelope{t: websocket.TextMessage, message: msg}
	p.hub.broadcast <- message

	return nil
}

// BroadcastFilter 向符合过滤器结果的会话广播消息.
func (p *Pigeon) BroadcastFilter(msg []byte, fn func(*Session) bool) error {
	if p.hub.closed() {
		return errors.New("pigeon instance is closed")
	}

	message := &envelope{t: websocket.TextMessage, message: msg, filter: fn}
	p.hub.broadcast <- message

	return nil
}

// BroadcastOthers 向某个会话之外的所有会话广播消息.
func (p *Pigeon) BroadcastOthers(msg []byte, s *Session) error {
	return p.BroadcastFilter(msg, func(q *Session) bool {
		return s != q
	})
}

// BroadcastMultiple 向多个会话广播消息.
func (p *Pigeon) BroadcastMultiple(msg []byte, sessions []*Session) error {
	for _, sess := range sessions {
		if writeErr := sess.Write(msg); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// BroadcastBinary 广播二进制消息.
func (p *Pigeon) BroadcastBinary(msg []byte) error {
	if p.hub.closed() {
		return errors.New("pigeon instance is closed")
	}
	message := &envelope{t: websocket.BinaryMessage, message: msg}
	p.hub.broadcast <- message
	return nil
}

// BroadcastBinaryFilter 向符合过滤器结果的会话广播二进制消息.
func (p *Pigeon) BroadcastBinaryFilter(msg []byte, fn func(*Session) bool) error {
	if p.hub.closed() {
		return errors.New("pigeon instance is closed")
	}

	message := &envelope{t: websocket.BinaryMessage, message: msg, filter: fn}
	p.hub.broadcast <- message

	return nil
}

// BroadcastBinaryOthers 向某个会话之外的所有会话广播二进制消息.
func (p *Pigeon) BroadcastBinaryOthers(msg []byte, s *Session) error {
	return p.BroadcastBinaryFilter(msg, func(q *Session) bool {
		return s != q
	})
}

// Range 遍历所有session
func (p *Pigeon) Range(fn func(*Session) bool) {
	if fn == nil {
		return
	}
	p.hub.iterator(fn)
}

// Close 关闭信鸽以及所有会话的连接.
func (p *Pigeon) Close() error {
	return p.CloseWithMsg([]byte{})
}

// CloseWithMsg 关闭信鸽以及所有会话的连接，并向客户端发送消息
func (p *Pigeon) CloseWithMsg(msg []byte) error {
	if p.hub.closed() {
		return errors.New("pigeon instance is already closed")
	}
	p.hub.exit <- &envelope{t: websocket.CloseMessage, message: msg}
	return nil
}

// Len 获取会话连接数量.
func (p *Pigeon) Len() int {
	return p.hub.len()
}

// IsClosed 判断信鸽实例的状态.
func (p *Pigeon) IsClosed() bool {
	return p.hub.closed()
}
