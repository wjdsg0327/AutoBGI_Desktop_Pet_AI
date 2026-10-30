// 芙宁娜桌宠是独立客户端，只通过 API 密钥连接日志与触发本地动作。
package main

import (
	"embed"
	"log"
	"net/http"

	"autobgi-pet/internal/bindings"
	"autobgi-pet/internal/desktop"
	"autobgi-pet/internal/service"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 构建前由 Vite 生成可信静态资源，不在 WebView 内加载远程 AutoBGI 页面。
//
//go:embed all:frontend/dist
var assets embed.FS

// 托盘使用本版本生成的芙宁娜主题图标。
//
//go:embed assets/furina-icon.ico
var icon []byte

// main 接线唯一业务服务和桌面宿主，生命周期与采集协程在退出时统一清理。
func main() {
	srv, err := service.New()
	if err != nil {
		log.Print("桌宠配置初始化失败，请检查当前用户应用数据目录权限")
		return
	}
	desktopService := desktop.New(srv)
	app := application.New(application.Options{
		Name: "AutoBGI Pet", Description: "芙宁娜桌宠 · 日志陪伴", Icon: icon,
		Windows: desktopService.HostWindowsOptions(),
		// 只绑定本版所需的连接、规则和窗口方法，旧业务及宿主内部回调不注册到 WebView。
		Services: []application.Service{application.NewService(bindings.NewPetAPI(srv)), application.NewService(bindings.NewDesktopAPI(desktopService))},
		Assets: application.AssetOptions{Handler: application.AssetFileServerFS(assets), Middleware: func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// 打包资源只允许同源脚本；所有远程文本按纯文本渲染，前端不持有网络凭据。
				w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; frame-src 'none'; base-uri 'none'; form-action 'none'")
				w.Header().Set("X-Content-Type-Options", "nosniff")
				w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), clipboard-read=(), display-capture=(), usb=(), serial=(), bluetooth=()")
				// 自定义皮肤只允许专用同源路由，不能把整个用户目录交给文件服务器。
				if srv.ServeSkinAsset(w, r) {
					return
				}
				next.ServeHTTP(w, r)
			})
		}},
		SingleInstance: &application.SingleInstanceOptions{UniqueID: "cn.autobgi.desktop-pet.v1", OnSecondInstanceLaunch: func(application.SecondInstanceData) {
			desktopService.RestoreFromHost()
		}},
		// 托盘创建失败是 Wails 的告警而非返回错误，先锁存后在 UI 线程打开可见恢复入口。
		WarningHandler: func(warning string) {
			log.Print(warning)
			desktopService.HandleHostWarning(warning)
		},
		OnShutdown: func() { srv.Shutdown(); desktopService.Shutdown() },
	})
	desktopService.Attach(app, icon)

	srv.SetEmitter(desktopService.Apply)
	// 关键词命中单独投递，避免状态快照合并时遗漏动作或台词。
	srv.SetLogTriggerEmitter(desktopService.QueueLogTrigger)
	// 拾取项目逐条交给桌面队列，避免日志快照合并时丢失蛋糕动作。
	srv.SetLogPickupEmitter(desktopService.QueueLogPickup)
	if err := app.Run(); err != nil {
		log.Print("桌宠运行失败，请确认 WebView2 已安装且桌面会话可用")
	}
}
