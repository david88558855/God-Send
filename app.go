package main

import (
	"context"
)

// App Wails应用绑定结构
type App struct {
	ctx context.Context
}

// NewApp 创建App实例
func NewApp() *App {
	return &App{}
}

// startup Wails启动回调
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// GetHTTPPort 获取HTTP端口（供前端调用）
func (a *App) GetHTTPPort() int {
	if appConfig != nil {
		return appConfig.GetHTTPPort()
	}
	return 53334
}

// GetDeviceName 获取设备名称（供前端调用）
func (a *App) GetDeviceName() string {
	if appConfig != nil {
		return appConfig.GetDeviceName()
	}
	return "未知设备"
}