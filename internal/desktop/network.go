package desktop

import (
	"errors"
	"net"
	"strings"
	"time"
)

// networkPollInterval 只定期读取本机接口，不产生外部请求；也覆盖原生注册失败或系统漏报的情况。
const networkPollInterval = 15 * time.Second

// networkFromInterfaces 判断本机是否存在工作中的非回环单播接口，不要求互联网或默认网关。
func networkFromInterfaces(interfaces []net.Interface, addresses func(net.Interface) ([]net.Addr, error)) (bool, error) {
	var readErr error
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		values, err := addresses(iface)
		if err != nil {
			readErr = err
			continue
		}
		for _, address := range values {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() && !ip.IsMulticast() {
				return true, nil
			}
		}
	}
	// 读取失败属于未知，不能把无权读取接口或系统暂时错误误判成明确断网。
	if readErr != nil {
		return false, readErr
	}
	return false, nil
}

// readNetworkAvailability 使用 Go 在 Windows 上的 GetAdaptersAddresses 实现，完全不连接远端服务。
func readNetworkAvailability() (bool, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return false, err
	}
	return networkFromInterfaces(interfaces, func(iface net.Interface) ([]net.Addr, error) { return iface.Addrs() })
}

// runNetworkUpdates 串行采样当前事实，忽略变更事件的旧 payload，测试可注入本地采样与时钟通道。
func runNetworkUpdates(done <-chan struct{}, changes <-chan struct{}, ticks <-chan time.Time, sample func() (bool, error), apply func(bool, error)) {
	for {
		select {
		case <-done:
			return
		case <-changes:
		case <-ticks:
		}
		// 退出优先于新采样，排队事件不能在宿主关闭后继续调用业务服务。
		select {
		case <-done:
			return
		default:
		}
		available, err := sample()
		select {
		case <-done:
			return
		default:
		}
		apply(available, err)
	}
}

// startNetworkMonitor 在返回前完成首次本机采样，确保 ApplicationStarted 中的 AutoConnect 看见初始离线状态。
func (d *DesktopService) startNetworkMonitor(changes <-chan struct{}, release func(), registrationErr error) {
	apply := func(available bool, err error) {
		if d.stopped() {
			return
		}
		warnings := []string{}
		if registrationErr != nil {
			warnings = append(warnings, "系统网络变更通知注册失败，已改为每 15 秒读取本机网络状态")
		}
		if err != nil {
			warnings = append(warnings, "暂时无法读取本机网络状态，保留上次判断；不会根据服务请求失败推断断网")
		}
		d.setNetworkWarning(strings.Join(warnings, "；"))
		if err == nil {
			d.service.SetNetworkAvailable(available)
		}
	}
	available, err := readNetworkAvailability()
	apply(available, err)
	d.networkWorker.Add(1)
	go func() {
		defer d.networkWorker.Done()
		defer release()
		ticker := time.NewTicker(networkPollInterval)
		defer ticker.Stop()
		runNetworkUpdates(d.done, changes, ticker.C, readNetworkAvailability, apply)
	}()
}

// setNetworkWarning 与快捷键、托盘告警分别保存，不覆盖其他恢复入口的状态。
func (d *DesktopService) setNetworkWarning(warning string) {
	d.mu.Lock()
	changed := d.networkWarning != warning
	d.networkWarning = warning
	d.mu.Unlock()
	if changed && d.app != nil && !d.stopped() {
		d.emitPresentation()
	}
}

// networkRegistrationError 隐去系统原始细节，仅让宿主明确显示已启用低频本地兜底。
func networkRegistrationError() error { return errors.New("本机网络通知不可用") }
