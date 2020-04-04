package pigeon

import "time"

// 信鸽的主要配置结构.
type Config struct {
	WriteWait         time.Duration // 写入超时时间.
	PongWait          time.Duration // 响应超时时间.
	PingPeriod        time.Duration // 两次ping之间的时间间隔.
	MaxMessageSize    int64         // 信息最大传输容量.
	MessageBufferSize int           // 缓冲区最大信息容量.
}

// 默认配置
func defaultConfig() *Config {
	return &Config{
		WriteWait:         10 * time.Second,
		PongWait:          60 * time.Second,
		PingPeriod:        (60 * time.Second * 9) / 10,
		MaxMessageSize:    512,
		MessageBufferSize: 256,
	}
}
