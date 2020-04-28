package pigeon

import (
	"errors"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Close codes defined in RFC 6455, section 11.7.
// Duplicate of codes from gorilla/websocket for convenience.
const (
	CloseNormalClosure           = 1000
	CloseGoingAway               = 1001
	CloseProtocolError           = 1002
	CloseUnsupportedData         = 1003
	CloseNoStatusReceived        = 1005
	CloseAbnormalClosure         = 1006
	CloseInvalidFramePayloadData = 1007
	ClosePolicyViolation         = 1008
	CloseMessageTooBig           = 1009
	CloseMandatoryExtension      = 1010
	CloseInternalServerErr       = 1011
	CloseServiceRestart          = 1012
	CloseTryAgainLater           = 1013
	CloseTLSHandshake            = 1015
)

// Duplicate of codes from gorilla/websocket for convenience.
var validReceivedCloseCodes = map[int]bool{
	CloseNormalClosure:           true,
	CloseGoingAway:               true,
	CloseProtocolError:           true,
	CloseUnsupportedData:         true,
	CloseNoStatusReceived:        false,
	CloseAbnormalClosure:         false,
	CloseInvalidFramePayloadData: true,
	ClosePolicyViolation:         true,
	CloseMessageTooBig:           true,
	CloseMandatoryExtension:      true,
	CloseInternalServerErr:       true,
	CloseServiceRestart:          true,
	CloseTryAgainLater:           true,
	CloseTLSHandshake:            false,
}

type handleMessageFunc func(*Session, []byte)
type handleErrorFunc func(*Session, error)
type handleCloseFunc func(*Session, int, string) error
type handleSessionFunc func(*Session)
type filterFunc func(*Session) bool

// websocket 管理器.
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

// 新建信鸽实例.
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

// 会话连接时的处理方法.
func (p *Pigeon) HandleConnect(fn func(*Session)) {
	p.connectHandler = fn
}

// 会话断开时的处理方法.
func (p *Pigeon) HandleDisconnect(fn func(*Session)) {
	p.disconnectHandler = fn
}

// 从会话中收到pong信息时的处理方法.
func (p *Pigeon) HandlePong(fn func(*Session)) {
	p.pongHandler = fn
}

// 收到信息时的处理方法.
func (p *Pigeon) HandleMessage(fn func(*Session, []byte)) {
	p.messageHandler = fn
}

// 收到二进制信息的处理方法.
func (p *Pigeon) HandleMessageBinary(fn func(*Session, []byte)) {
	p.messageHandlerBinary = fn
}

// 发送信息时的处理方法.
func (p *Pigeon) HandleSentMessage(fn func(*Session, []byte)) {
	p.messageSentHandler = fn
}

// 发送二进制信息的处理方法.
func (p *Pigeon) HandleSentMessageBinary(fn func(*Session, []byte)) {
	p.messageSentHandlerBinary = fn
}

// 发生错误时的处理方法.
func (p *Pigeon) HandleError(fn func(*Session, error)) {
	p.errorHandler = fn
}

// 信鸽关闭时的处理方法
func (p *Pigeon) HandleClose(fn func(*Session, int, string) error) {
	if fn != nil {
		p.closeHandler = fn
	}
}

// 将http请求升级成websocket连接，并将其注册到信鸽实例进行管理.
func (p *Pigeon) HandleRequest(w http.ResponseWriter, r *http.Request) error {
	return p.HandleRequestWithKeys(w, r, nil)
}

// 与HandleRequest功能相同，增加keys.
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

	go session.writePump()

	session.readPump()

	if !p.hub.closed() {
		p.hub.unregister <- session
	}

	session.close()

	p.disconnectHandler(session)

	return nil
}

// 广播消息.
func (p *Pigeon) Broadcast(msg []byte) error {
	if p.hub.closed() {
		return errors.New("pigeon instance is closed")
	}

	message := &envelope{t: websocket.TextMessage, message: msg}
	p.hub.broadcast <- message

	return nil
}

// 向符合过滤器结果的会话广播消息.
func (p *Pigeon) BroadcastFilter(msg []byte, fn func(*Session) bool) error {
	if p.hub.closed() {
		return errors.New("pigeon instance is closed")
	}

	message := &envelope{t: websocket.TextMessage, message: msg, filter: fn}
	p.hub.broadcast <- message

	return nil
}

// 向某个会话之外的所有会话广播消息.
func (p *Pigeon) BroadcastOthers(msg []byte, s *Session) error {
	return p.BroadcastFilter(msg, func(q *Session) bool {
		return s != q
	})
}

// 向多个会话广播消息.
func (p *Pigeon) BroadcastMultiple(msg []byte, sessions []*Session) error {
	for _, sess := range sessions {
		if writeErr := sess.Write(msg); writeErr != nil {
			return writeErr
		}
	}
	return nil
}

// 广播二进制消息.
func (p *Pigeon) BroadcastBinary(msg []byte) error {
	if p.hub.closed() {
		return errors.New("pigeon instance is closed")
	}

	message := &envelope{t: websocket.BinaryMessage, message: msg}
	p.hub.broadcast <- message

	return nil
}

// 向符合过滤器结果的会话广播二进制消息.
func (p *Pigeon) BroadcastBinaryFilter(msg []byte, fn func(*Session) bool) error {
	if p.hub.closed() {
		return errors.New("pigeon instance is closed")
	}

	message := &envelope{t: websocket.BinaryMessage, message: msg, filter: fn}
	p.hub.broadcast <- message

	return nil
}

// 向某个会话之外的所有会话广播二进制消息.
func (p *Pigeon) BroadcastBinaryOthers(msg []byte, s *Session) error {
	return p.BroadcastBinaryFilter(msg, func(q *Session) bool {
		return s != q
	})
}

// 过去某个单一的session
func (p *Pigeon) FilterSession(fn func(*Session) bool) *Session {
	if fn == nil {
		return nil
	}
	return p.hub.filterSession(fn)
}

// 关闭信鸽以及所有会话的连接.
func (p *Pigeon) Close() error {
	if p.hub.closed() {
		return errors.New("pigeon instance is already closed")
	}

	p.hub.exit <- &envelope{t: websocket.CloseMessage, message: []byte{}}

	return nil
}

// 关闭信鸽以及所有会话的连接，并向客户端发送消息
func (p *Pigeon) CloseWithMsg(msg []byte) error {
	if p.hub.closed() {
		return errors.New("pigeon instance is already closed")
	}

	p.hub.exit <- &envelope{t: websocket.CloseMessage, message: msg}

	return nil
}

// 获取会话连接数量.
func (p *Pigeon) Len() int {
	return p.hub.len()
}

// 判断信鸽实例的状态.
func (p *Pigeon) IsClosed() bool {
	return p.hub.closed()
}

// 格式化会话关闭时的信息.
func FormatCloseMessage(closeCode int, text string) []byte {
	return websocket.FormatCloseMessage(closeCode, text)
}
