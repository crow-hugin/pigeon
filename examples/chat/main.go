package main

import (
	"fmt"
	"github.com/crow-hugin/pigeon"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"net/http"
)

func main() {
	r := gin.Default()
	m := pigeon.New(nil)

	r.GET("/", func(c *gin.Context) {
		http.ServeFile(c.Writer, c.Request, "index.html")
	})

	r.GET("/ws", func(c *gin.Context) {
		m.HandleRequest(c.Writer, c.Request)
	})

	m.HandleMessage(func(s *pigeon.Session, msg []byte) {
		m.Broadcast(msg)
	})
	m.HandleConnect(func(session *pigeon.Session) {
		fmt.Println("有新的链接")
	})

	m.HandleDisconnect(func(session *pigeon.Session) {
		fmt.Println("会员掉线")
	})
	m.HandleClose(func(session *pigeon.Session, i int, s string) error {
		return session.CloseWithMsg(websocket.FormatCloseMessage(i, s))
	})
	m.HandleError(func(session *pigeon.Session, err error) {
		fmt.Println("发生错误: ", err)
	})

	r.Run(":5555")
}
