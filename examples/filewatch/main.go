package main

import (
	"github.com/crow-hugin/pigeon"
	"github.com/fsnotify/fsnotify"
	"github.com/gin-gonic/gin"
	"io/ioutil"
	"net/http"
)

func main() {
	file := "file.txt"

	r := gin.Default()
	m := pigeon.New()
	w, _ := fsnotify.NewWatcher()

	r.GET("/", func(c *gin.Context) {
		http.ServeFile(c.Writer, c.Request, "index.html")
	})

	r.GET("/ws", func(c *gin.Context) {
		m.HandleRequest(c.Writer, c.Request)
	})

	m.HandleConnect(func(s *pigeon.Session) {
		content, _ := ioutil.ReadFile(file)
		s.Write(content)
	})

	go func() {
		for {
			ev := <-w.Events
			if ev.Op == fsnotify.Write {
				content, _ := ioutil.ReadFile(ev.Name)
				m.Broadcast(content)
			}
		}
	}()

	w.Add(file)

	r.Run(":5000")
}
