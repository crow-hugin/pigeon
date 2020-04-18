package main

import (
	"github.com/crow-hugin/pigeon"
	"github.com/gin-gonic/gin"
	"net/http"
)

func main() {
	r := gin.Default()
	m := pigeon.New()

	r.GET("/", func(c *gin.Context) {
		http.ServeFile(c.Writer, c.Request, "index.html")
	})

	r.GET("/ws", func(c *gin.Context) {
		m.HandleRequest(c.Writer, c.Request)
	})

	m.HandleMessage(func(s *pigeon.Session, msg []byte) {
		m.Broadcast(msg)
	})

	r.Run(":5000")
}
