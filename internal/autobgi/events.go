package autobgi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CompareEventCursors 只比较同一进程 epoch 内的十进制序号，跨 epoch 必须重新同步快照。
func CompareEventCursors(current, next string) (int, error) {
	parse := func(value string) (string, uint64, bool) {
		epoch, number, ok := strings.Cut(value, ":")
		sequence, err := strconv.ParseUint(number, 10, 64)
		return epoch, sequence, ok && epoch != "" && err == nil && !strings.ContainsAny(epoch, "\r\n\x00") && len(value) <= 512
	}
	oldEpoch, oldSequence, oldOK := parse(current)
	newEpoch, newSequence, newOK := parse(next)
	if !oldOK || !newOK {
		return 0, &APIError{Code: "INVALID_RESPONSE", Message: "事件游标格式无效"}
	}
	if oldEpoch != newEpoch {
		return 0, &APIError{Code: "RESYNC_REQUIRED", Message: "事件服务代次已经变化，需要重新同步完整快照"}
	}
	if newSequence < oldSequence {
		return -1, nil
	}
	if newSequence == oldSequence {
		return 0, nil
	}
	return 1, nil
}

// readSSEFrame 限制单行和单帧大小，支持分片、CRLF、多行 data 与心跳注释。
func readSSEFrames(reader io.Reader, handle func(string, string) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 256*1024)
	var id, kind string
	var data []string
	size := 0
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if len(data) > 0 {
				if err := handle(id, kind+"\x00"+strings.Join(data, "\n")); err != nil {
					return err
				}
			}
			id, kind, data, size = "", "", nil, 0
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			if strings.ContainsRune(value, '\x00') || len(value) > 512 {
				return &APIError{Code: "INVALID_RESPONSE", Message: "事件标识格式无效"}
			}
			id = value
		case "event":
			kind = value
		case "data":
			size += len(value)
			if size > 256*1024 {
				return &APIError{Code: "INVALID_RESPONSE", Message: "事件内容超过安全限制"}
			}
			data = append(data, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return &APIError{Code: "NETWORK_ERROR", Message: "事件连接中断或超过安全大小限制"}
	}
	// 没有空行结尾的半帧不能推进游标，下次连接由服务端重新发送完整事件。
	return io.EOF
}

// activityReader 在收到任意字节时续期，心跳也能维持连接而不依赖业务事件频率。
type activityReader struct {
	reader io.Reader
	touch  func()
}

// Read 保留底层读取语义，仅在有实际流量时更新空闲看门狗。
func (r activityReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.touch()
	}
	return n, err
}

// Subscribe 使用独立长连接客户端且禁止重定向，密钥只存在于 Go 请求头。
func (a *PetV1Adapter) Subscribe(ctx context.Context, query EventQuery, emit func(Event)) error {
	if !a.caps.Events || query.AccountID != nil {
		return unsupported()
	}
	u := *a.client.base
	u.Path = strings.TrimRight(u.Path, "/") + "/api/desktop-pet/v1/events"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return &APIError{Code: "INVALID_ARGUMENT", Message: "无法建立事件连接"}
	}
	req.Header.Set("apiKey", a.client.key)
	req.Header.Set("Accept", "text/event-stream")
	if query.Cursor != nil {
		if len(*query.Cursor) > 512 || strings.ContainsAny(*query.Cursor, "\r\n\x00") {
			return &APIError{Code: "INVALID_ARGUMENT", Message: "事件游标无效"}
		}
		req.Header.Set("Last-Event-ID", *query.Cursor)
	}
	// 共用受控传输层但去掉总请求超时，由连接 context 和空闲看门狗负责取消。
	longClient := &http.Client{Transport: a.client.http.Transport, CheckRedirect: a.client.http.CheckRedirect}
	response, err := longClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &APIError{Code: "NETWORK_ERROR", Message: "事件连接暂时不可用"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return decodeResponse(response, &struct{}{})
	}
	if !strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/event-stream") {
		return &APIError{Code: "INCOMPATIBLE_RESPONSE", Message: "服务未返回有效事件流"}
	}
	var timerMu sync.Mutex
	timer := time.AfterFunc(45*time.Second, func() { _ = response.Body.Close() })
	defer timer.Stop()
	touch := func() {
		timerMu.Lock()
		timer.Reset(45 * time.Second)
		timerMu.Unlock()
	}
	lastCursor := ""
	if query.Cursor != nil {
		lastCursor = *query.Cursor
	}
	err = readSSEFrames(activityReader{reader: response.Body, touch: touch}, func(id, payload string) error {
		kind, data, _ := strings.Cut(payload, "\x00")
		var event Event
		if json.Unmarshal([]byte(data), &event) != nil || event.SchemaVersion != 1 || id == "" || event.EventID != id || event.Type != kind || event.InstanceID != a.instanceID || event.AccountID != nil {
			return &APIError{Code: "INVALID_RESPONSE", Message: "事件身份或结构无效"}
		}
		if _, err := time.Parse(time.RFC3339Nano, event.OccurredAt); err != nil {
			return &APIError{Code: "INVALID_RESPONSE", Message: "事件发生时间无效"}
		}
		if lastCursor != "" {
			order, err := CompareEventCursors(lastCursor, event.EventID)
			if err != nil {
				return err
			}
			if order <= 0 {
				return nil
			}
		} else if _, err := CompareEventCursors(event.EventID, event.EventID); err != nil {
			return err
		}
		switch event.Type {
		case "snapshot.updated":
			// 快照事件只触发权威接口刷新，不直接把原始业务内容交给界面。
			event.Data = nil
		case "command.updated":
			var receipt CommandReceipt
			if json.Unmarshal(event.Data, &receipt) != nil {
				return &APIError{Code: "INVALID_RESPONSE", Message: "命令事件格式无效"}
			}
			clean, err := a.validateReceipt(receipt, receipt.CommandID)
			sameTask := (event.TaskID == nil && receipt.TaskID == nil) || (event.TaskID != nil && receipt.TaskID != nil && *event.TaskID == *receipt.TaskID)
			if err != nil || !validCommandID(receipt.CommandID) || !sameTask {
				return &APIError{Code: "INVALID_RESPONSE", Message: "命令事件内容无效"}
			}
			event.Data, _ = json.Marshal(clean)
		case "server.notice":
			// 服务提示仅保留受控类型，由本地文案说明，不展示原始远端文本。
			event.Data = nil
		default:
			return &APIError{Code: "INCOMPATIBLE_RESPONSE", Message: "收到未支持的事件类型"}
		}
		event.EventID = a.client.redact(event.EventID)
		event.InstanceID = a.client.redact(event.InstanceID)
		if event.TaskID != nil {
			value := a.client.redact(*event.TaskID)
			event.TaskID = &value
		}
		emit(event)
		lastCursor = event.EventID
		return nil
	})
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, io.EOF) {
		return &APIError{Code: "NETWORK_ERROR", Message: "事件连接已结束，正在恢复订阅"}
	}
	return err
}
