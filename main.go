package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// OpenAIRequest OpenAI API请求格式
type OpenAIRequest struct {
	Model            string                 `json:"model"`
	Messages         []Message              `json:"messages"`
	Temperature      *float64                `json:"temperature,omitempty"`
	TopP             *float64                `json:"top_p,omitempty"`
	MaxTokens        *int                   `json:"max_tokens,omitempty"`
	Stream           bool                   `json:"stream,omitempty"`
	PresencePenalty  *float64                `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64                `json:"frequency_penalty,omitempty"`
	User             string                 `json:"user,omitempty"`
	Stop             []string               `json:"stop,omitempty"`
	Functions        []interface{}          `json:"functions,omitempty"`
	FunctionCall     interface{}            `json:"function_call,omitempty"`
	ExtraBody        map[string]interface{} `json:"-"` // 用于存储其他未定义的字段
}

// Message 消息结构
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

// OpenAIResponse OpenAI API响应格式
type OpenAIResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice 选择项
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage 使用情况
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Config 配置结构
type Config struct {
	Port                string
	AppID               string
	APIKey              string
	BaseURL             string
	ProxyURL            string
	UseNative           bool   // 是否使用原生API格式
	RequestTimeout      int    // 非流式请求超时时间（秒）
	StreamTimeout       int    // 流式请求超时时间（秒）
	MaxIdleConns        int    // 最大空闲连接数
	MaxIdleConnsPerHost int    // 每个主机最大空闲连接数
	MaxConnsPerHost     int    // 每个主机最大连接数
	IdleConnTimeout     int    // 空闲连接超时时间（秒）
}

// AliyunNativeRequest 阿里云百炼原生API请求格式
type AliyunNativeRequest struct {
	Input      map[string]interface{} `json:"input"`
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	Debug      map[string]interface{} `json:"debug,omitempty"`
}

// AliyunNativeResponse 阿里云百炼原生API响应格式
type AliyunNativeResponse struct {
	Output struct {
		FinishReason string `json:"finish_reason"`
		RejectStatus bool   `json:"reject_status,omitempty"`
		SessionID    string `json:"session_id"`
		Text         string `json:"text"`
	} `json:"output"`
	Usage struct {
		Models []struct {
			InputTokens  int    `json:"input_tokens"`
			ModelID      string `json:"model_id"`
			OutputTokens int    `json:"output_tokens"`
		} `json:"models"`
	} `json:"usage"`
	RequestID string `json:"request_id"`
}

var config Config

// 全局HTTP客户端（复用连接，提高性能）
var httpClient *http.Client
var httpClientStream *http.Client // 流式请求专用客户端

func main() {
	// 加载配置
	loadConfig()

	// 初始化HTTP客户端（配置连接池以支持高并发）
	initHTTPClients()

	// 设置路由
	http.HandleFunc("/v1/chat/completions", handleChatCompletions)
	http.HandleFunc("/health", handleHealth)

	log.Printf("服务器启动，监听端口 %s", config.Port)
	log.Printf("阿里云百炼应用ID: %s", config.AppID)
	if config.UseNative {
		log.Printf("API端点: %s (原生API格式)", getAliyunNativeEndpoint())
	} else {
		log.Printf("API端点: %s (兼容模式)", getAliyunEndpoint())
	}
	
	if err := http.ListenAndServe(":"+config.Port, nil); err != nil {
		log.Fatalf("服务器启动失败: %v", err)
	}
}

// loadConfig 加载配置
func loadConfig() {
	config.Port = getEnv("PORT", "8080")
	config.AppID = getEnv("ALIYUN_APP_ID", "")
	config.APIKey = getEnv("ALIYUN_API_KEY", "")
	config.BaseURL = getEnv("ALIYUN_BASE_URL", "https://dashscope.aliyuncs.com")
	config.ProxyURL = getEnv("PROXY_URL", "")
	// 默认使用原生API格式（官方推荐）
	config.UseNative = getEnv("USE_NATIVE_API", "true") == "true"

	// 性能优化配置
	config.RequestTimeout = getEnvInt("REQUEST_TIMEOUT", 180)      // 非流式请求超时180秒（增加以支持长文本生成）
	config.StreamTimeout = getEnvInt("STREAM_TIMEOUT", 600)        // 流式请求超时600秒（增加以支持长文本流式生成）
	config.MaxIdleConns = getEnvInt("MAX_IDLE_CONNS", 100)         // 最大空闲连接数
	config.MaxIdleConnsPerHost = getEnvInt("MAX_IDLE_CONNS_PER_HOST", 50) // 每个主机最大空闲连接数
	config.MaxConnsPerHost = getEnvInt("MAX_CONNS_PER_HOST", 100)  // 每个主机最大连接数
	config.IdleConnTimeout = getEnvInt("IDLE_CONN_TIMEOUT", 90)    // 空闲连接超时90秒

	if config.AppID == "" {
		log.Fatal("错误: 必须设置 ALIYUN_APP_ID 环境变量")
	}
	if config.APIKey == "" {
		log.Fatal("错误: 必须设置 ALIYUN_API_KEY 环境变量")
	}
}

// getEnvInt 获取环境变量并转换为整数
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// initHTTPClients 初始化HTTP客户端（配置连接池）
func initHTTPClients() {
	// 创建自定义Transport，配置连接池
	// 注意：ResponseHeaderTimeout 应该大于或等于 Client.Timeout
	// 这里设置为0表示不限制，由Client.Timeout控制
	transport := &http.Transport{
		MaxIdleConns:        config.MaxIdleConns,
		MaxIdleConnsPerHost: config.MaxIdleConnsPerHost,
		MaxConnsPerHost:     config.MaxConnsPerHost,
		IdleConnTimeout:     time.Duration(config.IdleConnTimeout) * time.Second,
		DisableKeepAlives:   false, // 启用连接复用
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second, // 连接超时
			KeepAlive: 30 * time.Second, // Keep-Alive时间
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second, // TLS握手超时
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 0, // 0表示不限制，由Client.Timeout控制
	}

	// 非流式请求客户端（有超时限制）
	httpClient = &http.Client{
		Transport: transport,
		Timeout:   time.Duration(config.RequestTimeout) * time.Second,
	}

	// 流式请求客户端（超时时间更长，但不为0以避免资源泄漏）
	httpClientStream = &http.Client{
		Transport: transport,
		Timeout:   time.Duration(config.StreamTimeout) * time.Second,
	}

	log.Printf("HTTP客户端已初始化 - 最大空闲连接: %d, 每主机最大连接: %d, 请求超时: %ds, 流式超时: %ds", 
		config.MaxIdleConns, config.MaxConnsPerHost, config.RequestTimeout, config.StreamTimeout)
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getAliyunEndpoint 获取阿里云百炼API端点（兼容模式，已废弃）
func getAliyunEndpoint() string {
	// 兼容模式端点（可能不支持）
	return fmt.Sprintf("%s/api/v2/apps/agent/%s/compatible-mode/v1/chat/completions", config.BaseURL, config.AppID)
}

// getAliyunNativeEndpoint 获取阿里云百炼原生API端点（官方推荐）
func getAliyunNativeEndpoint() string {
	return fmt.Sprintf("%s/api/v1/apps/%s/completion", config.BaseURL, config.AppID)
}

// handleHealth 健康检查端点
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"service": "aliyun-bailian-proxy",
	})
}

// handleChatCompletions 处理聊天完成请求
// 流程：客户端(OpenAI格式) -> 转发站 -> 阿里云百炼(原生格式) -> 转发站 -> 客户端(OpenAI格式)
// 客户端完全不需要知道阿里云的原生格式，所有格式转换都在转发站完成
func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	// 只允许POST请求
	if r.Method != http.MethodPost {
		http.Error(w, "只支持POST请求", http.StatusMethodNotAllowed)
		return
	}

	// 读取请求体
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("读取请求体失败: %v", err)
		http.Error(w, "无法读取请求体", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 解析OpenAI请求
	var openAIReq OpenAIRequest
	if err := json.Unmarshal(body, &openAIReq); err != nil {
		log.Printf("解析请求失败: %v", err)
		http.Error(w, "请求格式错误: "+err.Error(), http.StatusBadRequest)
		return
	}

	// 验证必需字段
	if len(openAIReq.Messages) == 0 {
		http.Error(w, "messages字段不能为空", http.StatusBadRequest)
		return
	}

	var aliyunReqBody []byte
	var endpoint string

	if config.UseNative {
		// 使用原生API格式
		// 注意：原生API可能不支持流式响应，需要特殊处理
		aliyunReq := convertToNativeFormat(openAIReq)
		aliyunReqBody, err = json.Marshal(aliyunReq)
		endpoint = getAliyunNativeEndpoint()
	} else {
		// 使用兼容模式（OpenAI格式）
		aliyunReqBody, err = json.Marshal(openAIReq)
		endpoint = getAliyunEndpoint()
	}

	if err != nil {
		log.Printf("转换请求失败: %v", err)
		http.Error(w, "请求转换失败", http.StatusInternalServerError)
		return
	}

	// 限制日志长度，避免日志过长
	reqBodyStr := string(aliyunReqBody)
	if len(reqBodyStr) > 500 {
		reqBodyStr = reqBodyStr[:500] + "...(已截断)"
	}
	log.Printf("转发请求到阿里云百炼: %s", endpoint)
	log.Printf("请求内容: %s", reqBodyStr)

	// 创建HTTP请求
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(aliyunReqBody))
	if err != nil {
		log.Printf("创建请求失败: %v", err)
		http.Error(w, "创建请求失败", http.StatusInternalServerError)
		return
	}

	// 设置请求头
	req.Header.Set("Authorization", "Bearer "+config.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "aliyun-bailian-proxy/1.0")

	// 对于流式请求，设置Accept头
	if openAIReq.Stream {
		req.Header.Set("Accept", "text/event-stream")
	} else {
		req.Header.Set("Accept", "application/json")
	}

	// 复制原始请求的一些头信息（如果存在且不是流式请求）
	if accept := r.Header.Get("Accept"); accept != "" && !openAIReq.Stream {
		req.Header.Set("Accept", accept)
	}

	// 如果是流式请求，需要特殊处理
	if openAIReq.Stream {
		// 如果使用原生API，需要转换SSE格式
		if config.UseNative {
			handleStreamResponseNative(httpClientStream, req, w, openAIReq.Model)
		} else {
			handleStreamResponse(httpClientStream, req, w)
		}
		return
	}

	// 发送请求（使用全局客户端，复用连接）
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("请求失败: %v", err)
		
		// 检查是否是超时错误
		if strings.Contains(err.Error(), "timeout") {
			// 超时错误，返回504 Gateway Timeout
			errorResp := OpenAIErrorResponse{}
			errorResp.Error.Message = "请求超时，请稍后重试"
			errorResp.Error.Type = "timeout_error"
			errorJSON, _ := json.Marshal(errorResp)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusGatewayTimeout)
			w.Write(errorJSON)
		} else {
			// 其他错误
			errorResp := OpenAIErrorResponse{}
			errorResp.Error.Message = "无法连接到阿里云百炼API: " + err.Error()
			errorResp.Error.Type = "server_error"
			errorJSON, _ := json.Marshal(errorResp)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write(errorJSON)
		}
		return
	}
	defer resp.Body.Close()

	// 读取响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("读取响应失败: %v", err)
		http.Error(w, "读取响应失败", http.StatusInternalServerError)
		return
	}

	// 设置响应头
	w.Header().Set("Content-Type", "application/json")
	for key, values := range resp.Header {
		if strings.ToLower(key) == "content-type" || strings.ToLower(key) == "content-length" {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// 如果使用原生API格式，需要转换响应格式为OpenAI格式
	var finalRespBody []byte
	if config.UseNative {
		if resp.StatusCode == http.StatusOK {
			// 成功响应，转换为OpenAI格式
			convertedBody := convertNativeResponseToOpenAI(respBody, openAIReq.Model)
			if convertedBody != nil && len(convertedBody) > 0 {
				finalRespBody = convertedBody
				log.Printf("响应已转换为OpenAI格式")
			} else {
				// 转换失败，返回原始响应
				log.Printf("响应转换失败，返回原始响应")
				finalRespBody = respBody
			}
		} else {
			// 错误响应，转换为OpenAI错误格式
			convertedError := convertNativeErrorToOpenAI(respBody, resp.StatusCode)
			if convertedError != nil && len(convertedError) > 0 {
				finalRespBody = convertedError
				log.Printf("错误响应已转换为OpenAI格式")
			} else {
				// 转换失败，返回原始响应
				finalRespBody = respBody
			}
		}
	} else {
		finalRespBody = respBody
	}

	// 返回响应状态码和内容
	w.WriteHeader(resp.StatusCode)
	w.Write(finalRespBody)

	log.Printf("响应状态码: %d", resp.StatusCode)
}

// convertToNativeFormat 将OpenAI请求格式转换为阿里云百炼原生API格式
// 这是内部转换，客户端不需要知道原生格式
func convertToNativeFormat(openAIReq OpenAIRequest) AliyunNativeRequest {
	// 构建input字段
	// 根据官方文档，可以使用 prompt 或 messages
	input := make(map[string]interface{})
	
	// 如果只有一条user消息，使用prompt字段
	if len(openAIReq.Messages) == 1 && openAIReq.Messages[0].Role == "user" {
		input["prompt"] = openAIReq.Messages[0].Content
	} else {
		// 多条消息或包含system/assistant消息，使用messages字段
		// 将OpenAI格式的messages转换为阿里云格式
		aliyunMessages := make([]map[string]interface{}, 0, len(openAIReq.Messages))
		for _, msg := range openAIReq.Messages {
			aliyunMsg := map[string]interface{}{
				"role":    msg.Role,
				"content": msg.Content,
			}
			if msg.Name != "" {
				aliyunMsg["name"] = msg.Name
			}
			aliyunMessages = append(aliyunMessages, aliyunMsg)
		}
		input["messages"] = aliyunMessages
	}
	
	// 构建parameters
	parameters := make(map[string]interface{})
	if openAIReq.Temperature != nil {
		parameters["temperature"] = *openAIReq.Temperature
	}
	if openAIReq.TopP != nil {
		parameters["top_p"] = *openAIReq.TopP
	}
	if openAIReq.MaxTokens != nil {
		parameters["max_tokens"] = *openAIReq.MaxTokens
	}
	if len(openAIReq.Stop) > 0 {
		parameters["stop"] = openAIReq.Stop
	}
	if openAIReq.PresencePenalty != nil {
		parameters["presence_penalty"] = *openAIReq.PresencePenalty
	}
	if openAIReq.FrequencyPenalty != nil {
		parameters["frequency_penalty"] = *openAIReq.FrequencyPenalty
	}
	
	// 如果parameters为空，设置为空对象而不是nil
	if len(parameters) == 0 {
		parameters = make(map[string]interface{})
	}
	
	return AliyunNativeRequest{
		Input:      input,
		Parameters: parameters,
		Debug:      make(map[string]interface{}),
	}
}

// convertNativeResponseToOpenAI 将阿里云百炼原生API响应转换为OpenAI格式
func convertNativeResponseToOpenAI(nativeRespBody []byte, model string) []byte {
	var nativeResp AliyunNativeResponse
	if err := json.Unmarshal(nativeRespBody, &nativeResp); err != nil {
		log.Printf("解析原生响应失败: %v，返回原始响应", err)
		log.Printf("原始响应内容: %s", string(nativeRespBody))
		return nil
	}

	// 计算总token数
	totalTokens := 0
	inputTokens := 0
	outputTokens := 0
	if len(nativeResp.Usage.Models) > 0 {
		inputTokens = nativeResp.Usage.Models[0].InputTokens
		outputTokens = nativeResp.Usage.Models[0].OutputTokens
		totalTokens = inputTokens + outputTokens
	}

	// 处理finish_reason，确保符合OpenAI格式
	finishReason := nativeResp.Output.FinishReason
	if finishReason == "" {
		finishReason = "stop"
	}

	// 使用当前时间戳作为Created字段
	created := time.Now().Unix()

	// 构建OpenAI格式的响应
	openAIResp := OpenAIResponse{
		ID:      nativeResp.RequestID,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: nativeResp.Output.Text,
				},
				FinishReason: finishReason,
			},
		},
		Usage: Usage{
			PromptTokens:     inputTokens,
			CompletionTokens: outputTokens,
			TotalTokens:      totalTokens,
		},
	}

	result, err := json.Marshal(openAIResp)
	if err != nil {
		log.Printf("转换响应格式失败: %v，返回原始响应", err)
		return nil
	}

	log.Printf("成功转换响应格式，input_tokens: %d, output_tokens: %d", inputTokens, outputTokens)
	return result
}

// AliyunErrorResponse 阿里云错误响应格式
type AliyunErrorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

// OpenAIErrorResponse OpenAI错误响应格式
type OpenAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code,omitempty"`
	} `json:"error"`
}

// parseSSEError 从SSE格式中提取JSON错误数据
func parseSSEError(sseBody []byte) []byte {
	bodyStr := string(sseBody)
	
	// 首先检查是否是纯JSON格式
	trimmed := strings.TrimSpace(bodyStr)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		return sseBody
	}
	
	// 尝试解析SSE格式
	lines := strings.Split(bodyStr, "\n")
	
	// 查找所有data:行，找到包含JSON的那个
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "data:") {
			jsonStr := strings.TrimPrefix(line, "data:")
			jsonStr = strings.TrimSpace(jsonStr)
			// 检查是否是JSON格式（以{开头，以}结尾）
			if strings.HasPrefix(jsonStr, "{") {
				// 找到完整的JSON对象
				braceCount := 0
				jsonEnd := -1
				for j, char := range jsonStr {
					if char == '{' {
						braceCount++
					} else if char == '}' {
						braceCount--
						if braceCount == 0 {
							jsonEnd = j + 1
							break
						}
					}
				}
				if jsonEnd > 0 {
					return []byte(jsonStr[:jsonEnd])
				}
				return []byte(jsonStr)
			}
		}
	}
	
	// 如果没找到，尝试在整个body中查找JSON对象
	if idx := strings.Index(bodyStr, "{"); idx >= 0 {
		// 从第一个{开始查找完整的JSON
		jsonStart := idx
		braceCount := 0
		jsonEnd := -1
		for i := jsonStart; i < len(bodyStr); i++ {
			if bodyStr[i] == '{' {
				braceCount++
			} else if bodyStr[i] == '}' {
				braceCount--
				if braceCount == 0 {
					jsonEnd = i + 1
					break
				}
			}
		}
		if jsonEnd > 0 {
			return []byte(bodyStr[jsonStart:jsonEnd])
		}
	}
	
	return nil
}

// convertNativeErrorToOpenAI 将阿里云错误响应转换为OpenAI错误格式
func convertNativeErrorToOpenAI(errorBody []byte, statusCode int) []byte {
	// 首先尝试解析SSE格式
	jsonBody := parseSSEError(errorBody)
	if jsonBody == nil {
		// 如果不是SSE格式，直接使用原始body
		jsonBody = errorBody
	}
	
	var aliyunError AliyunErrorResponse
	if err := json.Unmarshal(jsonBody, &aliyunError); err != nil {
		log.Printf("解析错误响应失败: %v", err)
		log.Printf("原始响应内容: %s", string(errorBody))
		// 如果解析失败，尝试从响应中提取错误信息
		bodyStr := string(errorBody)
		if strings.Contains(bodyStr, "\"message\"") {
			// 尝试提取message字段
			if idx := strings.Index(bodyStr, "\"message\""); idx > 0 {
				// 简单提取，如果失败则返回通用错误
				aliyunError.Message = "API请求失败"
				aliyunError.Code = "api_error"
			}
		} else {
			return nil
		}
	}

	// 构建OpenAI格式的错误响应
	openAIError := OpenAIErrorResponse{}
	openAIError.Error.Message = aliyunError.Message
	openAIError.Error.Code = aliyunError.Code
	
	// 根据状态码设置错误类型
	switch statusCode {
	case 400:
		openAIError.Error.Type = "invalid_request_error"
	case 401:
		openAIError.Error.Type = "authentication_error"
	case 403:
		openAIError.Error.Type = "permission_error"
	case 404:
		openAIError.Error.Type = "invalid_request_error"
	case 429:
		openAIError.Error.Type = "rate_limit_error"
	case 500, 502, 503:
		openAIError.Error.Type = "server_error"
	default:
		openAIError.Error.Type = "api_error"
	}

	result, err := json.Marshal(openAIError)
	if err != nil {
		log.Printf("转换错误响应格式失败: %v", err)
		return nil
	}

	log.Printf("错误响应已转换为OpenAI格式: %s", aliyunError.Message)
	return result
}

// handleStreamResponse 处理流式响应（兼容模式，直接转发）
func handleStreamResponse(client *http.Client, req *http.Request, w http.ResponseWriter) {
	// 设置流式响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // 禁用nginx缓冲

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("流式请求失败: %v", err)
		http.Error(w, "无法连接到阿里云百炼API", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	// 如果响应状态码不是200，需要转换为OpenAI错误格式
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		errorMsg := fmt.Sprintf("data: %s\n\n", string(body))
		w.WriteHeader(resp.StatusCode)
		w.Write([]byte(errorMsg))
		return
	}

	// 流式传输响应
	buffer := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				log.Printf("写入响应失败: %v", writeErr)
				return
			}
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("读取流式响应失败: %v", err)
			return
		}
	}
}

// handleStreamResponseNative 处理原生API的流式响应，转换SSE格式
func handleStreamResponseNative(client *http.Client, req *http.Request, w http.ResponseWriter, model string) {
	// 设置流式响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// 发送请求
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("流式请求失败: %v", err)
		errorResp := OpenAIErrorResponse{}
		errorResp.Error.Message = "无法连接到阿里云百炼API: " + err.Error()
		errorResp.Error.Type = "server_error"
		errorJSON, _ := json.Marshal(errorResp)
		fmt.Fprintf(w, "data: %s\n\n", string(errorJSON))
		return
	}
	defer resp.Body.Close()

	// 如果响应状态码不是200，转换为OpenAI错误格式
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		convertedError := convertNativeErrorToOpenAI(body, resp.StatusCode)
		if convertedError != nil && len(convertedError) > 0 {
			fmt.Fprintf(w, "data: %s\n\n", string(convertedError))
		} else {
			fmt.Fprintf(w, "data: %s\n\n", string(body))
		}
		return
	}

	// 解析SSE流式响应并转换格式
	scanner := bufio.NewScanner(resp.Body)
	var lastText string
	var requestID string
	var created int64 = time.Now().Unix()
	
	for scanner.Scan() {
		line := scanner.Text()
		
		// 解析SSE格式
		if strings.HasPrefix(line, "data:") {
			jsonStr := strings.TrimPrefix(line, "data:")
			jsonStr = strings.TrimSpace(jsonStr)
			
			// 跳过空行和非JSON数据
			if jsonStr == "" || !strings.HasPrefix(jsonStr, "{") {
				continue
			}
			
			// 尝试解析为阿里云响应格式
			var nativeResp AliyunNativeResponse
			if err := json.Unmarshal([]byte(jsonStr), &nativeResp); err != nil {
				// 解析失败，跳过
				log.Printf("解析SSE数据失败: %v, 数据: %s", err, jsonStr[:min(100, len(jsonStr))])
				continue
			}
			
			// 提取request_id（第一次）
			if requestID == "" && nativeResp.RequestID != "" {
				requestID = nativeResp.RequestID
			}
			
			// 获取当前文本内容
			currentText := nativeResp.Output.Text
			
			// 计算增量内容
			if len(currentText) > len(lastText) {
				delta := currentText[len(lastText):]
				lastText = currentText
				
				// 转换为OpenAI格式的SSE
				chunkResp := map[string]interface{}{
					"id":      requestID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   model,
					"choices": []map[string]interface{}{
						{
							"index": 0,
							"delta": map[string]interface{}{
								"content": delta,
							},
							"finish_reason": nil,
						},
					},
				}
				
				chunkJSON, _ := json.Marshal(chunkResp)
				fmt.Fprintf(w, "data: %s\n\n", string(chunkJSON))
				
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
			
			// 如果finish_reason不是null或空，发送完成消息
			finishReason := nativeResp.Output.FinishReason
			if finishReason != "" && finishReason != "null" {
				// 构建最终chunk，包含finish_reason和usage信息
				finalChunk := map[string]interface{}{
					"id":      requestID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   model,
					"choices": []map[string]interface{}{
						{
							"index":        0,
							"delta":        map[string]interface{}{},
							"finish_reason": finishReason,
						},
					},
				}
				
				// 如果有usage信息，添加到finalChunk中
				if len(nativeResp.Usage.Models) > 0 {
					finalChunk["usage"] = map[string]interface{}{
						"prompt_tokens":     nativeResp.Usage.Models[0].InputTokens,
						"completion_tokens": nativeResp.Usage.Models[0].OutputTokens,
						"total_tokens":      nativeResp.Usage.Models[0].InputTokens + nativeResp.Usage.Models[0].OutputTokens,
					}
				}
				
				finalJSON, _ := json.Marshal(finalChunk)
				fmt.Fprintf(w, "data: %s\n\n", string(finalJSON))
				
				// 发送结束标记
				fmt.Fprintf(w, "data: [DONE]\n\n")
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
				break
			}
		}
	}
	
	if err := scanner.Err(); err != nil {
		log.Printf("读取流式响应失败: %v", err)
	}
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// handleStreamResponseForNative 处理原生API的流式响应（模拟流式）
// 由于原生API可能不支持流式，我们需要将非流式响应转换为SSE格式
func handleStreamResponseForNative(client *http.Client, req *http.Request, w http.ResponseWriter, model string) {
	// 设置流式响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	// 发送请求（非流式）
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("流式请求失败: %v", err)
		// 返回SSE格式的错误
		errorResp := OpenAIErrorResponse{}
		errorResp.Error.Message = "无法连接到阿里云百炼API: " + err.Error()
		errorResp.Error.Type = "server_error"
		errorJSON, _ := json.Marshal(errorResp)
		fmt.Fprintf(w, "data: %s\n\n", string(errorJSON))
		return
	}
	defer resp.Body.Close()

	// 如果响应状态码不是200，转换为OpenAI错误格式
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		convertedError := convertNativeErrorToOpenAI(body, resp.StatusCode)
		if convertedError != nil && len(convertedError) > 0 {
			fmt.Fprintf(w, "data: %s\n\n", string(convertedError))
		} else {
			fmt.Fprintf(w, "data: %s\n\n", string(body))
		}
		return
	}

	// 读取完整响应
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("读取响应失败: %v", err)
		errorResp := OpenAIErrorResponse{}
		errorResp.Error.Message = "读取响应失败"
		errorResp.Error.Type = "server_error"
		errorJSON, _ := json.Marshal(errorResp)
		fmt.Fprintf(w, "data: %s\n\n", string(errorJSON))
		return
	}

	// 转换为OpenAI格式
	openAIResp := convertNativeResponseToOpenAI(respBody, model)
	if openAIResp == nil || len(openAIResp) == 0 {
		// 转换失败，返回错误
		errorResp := OpenAIErrorResponse{}
		errorResp.Error.Message = "响应格式转换失败"
		errorResp.Error.Type = "server_error"
		errorJSON, _ := json.Marshal(errorResp)
		fmt.Fprintf(w, "data: %s\n\n", string(errorJSON))
		return
	}

	// 解析OpenAI响应以便分块发送
	var openAIRespObj OpenAIResponse
	if err := json.Unmarshal(openAIResp, &openAIRespObj); err != nil {
		// 如果解析失败，直接发送完整响应
		fmt.Fprintf(w, "data: %s\n\n", string(openAIResp))
		fmt.Fprintf(w, "data: [DONE]\n\n")
		return
	}

	// 将响应内容分块发送（模拟流式）
	content := openAIRespObj.Choices[0].Message.Content
	if len(content) > 0 {
		// 分块大小（字符数）
		chunkSize := 10
		for i := 0; i < len(content); i += chunkSize {
			end := i + chunkSize
			if end > len(content) {
				end = len(content)
			}
			
			chunk := content[i:end]
			chunkResp := OpenAIResponse{
				ID:      openAIRespObj.ID,
				Object:  "chat.completion.chunk",
				Created: openAIRespObj.Created,
				Model:   openAIRespObj.Model,
				Choices: []Choice{
					{
						Index: 0,
						Message: Message{
							Role:    "assistant",
							Content: chunk,
						},
						FinishReason: "",
					},
				},
			}
			
			chunkJSON, _ := json.Marshal(chunkResp)
			fmt.Fprintf(w, "data: %s\n\n", string(chunkJSON))
			
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			
			// 添加小延迟以模拟真实流式响应
			time.Sleep(10 * time.Millisecond)
		}
	}

	// 发送完成消息
	finalResp := OpenAIResponse{
		ID:      openAIRespObj.ID,
		Object:  "chat.completion.chunk",
		Created: openAIRespObj.Created,
		Model:   openAIRespObj.Model,
		Choices: []Choice{
			{
				Index:        0,
				Message:      Message{Role: "assistant", Content: ""},
				FinishReason: openAIRespObj.Choices[0].FinishReason,
			},
		},
	}
	finalJSON, _ := json.Marshal(finalResp)
	fmt.Fprintf(w, "data: %s\n\n", string(finalJSON))

	// 发送usage信息（如果支持）
	if openAIRespObj.Usage.TotalTokens > 0 {
		usageResp := map[string]interface{}{
			"id":    openAIRespObj.ID,
			"object": "chat.completion.chunk",
			"usage":  openAIRespObj.Usage,
		}
		usageJSON, _ := json.Marshal(usageResp)
		fmt.Fprintf(w, "data: %s\n\n", string(usageJSON))
	}

	// 发送结束标记
	fmt.Fprintf(w, "data: [DONE]\n\n")
}

