package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/log"

	"github.com/charmbracelet/glamour"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcphost/pkg/history"
	"github.com/mark3labs/mcphost/pkg/llm"
	"github.com/mark3labs/mcphost/pkg/llm/anthropic"
	"github.com/mark3labs/mcphost/pkg/llm/google"
	"github.com/mark3labs/mcphost/pkg/llm/ollama"
	"github.com/mark3labs/mcphost/pkg/llm/openai"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// 定义全局变量，用于存储命令行参数或配置文件传入的值
var (
	renderer         *glamour.TermRenderer // 用于终端 Markdown 渲染
	configFile       string                // 配置文件路径
	systemPromptFile string                // 系统提示词文件路径
	messageWindow    int                   // 上下文中保留的消息条数
	modelFlag        string                // 模型选择参数，如 "openai:gpt-4"
	openaiBaseURL    string                // OpenAI API 的基础 URL
	anthropicBaseURL string                // Anthropic API 的基础 URL
	openaiAPIKey     string                // OpenAI API 密钥
	anthropicAPIKey  string                // Anthropic API 密钥
	googleAPIKey     string                // Google Gemini API 密钥
)

// 定义常量用于控制重试策略
const (
	initialBackoff = 1 * time.Second  // 初始退避时间
	maxBackoff     = 30 * time.Second // 最大退避时间
	maxRetries     = 5                // 最多重试次数
)

// 创建 root 命令（主命令）
var rootCmd = &cobra.Command{
	Use:   "mcphost",                                         // 程序名称
	Short: "Chat with AI models through a unified interface", // 简短描述
	Long: `MCPHost 是一个 CLI 工具，用于与不同 AI 模型统一交互。
它支持通过 MCP 服务器连接多种 AI 模型，并提供流式响应能力。

可用模型可通过 --model 参数指定，例如：
- Anthropic Claude（默认）：anthropic:claude-3-5-sonnet-latest
- OpenAI：openai:gpt-4
- Ollama 本地模型：ollama:modelname
- Google Gemini：google:modelname

示例：
  mcphost -m ollama:qwen2.5:3b
  mcphost -m openai:gpt-4
  mcphost -m google:gemini-2.0-flash`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 执行主逻辑（定义在 runMCPHost 中）
		return runMCPHost(context.Background())
	},
}

// 执行 root 命令，CLI 程序从这里启动
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var debugMode bool // 是否启用调试模式

// 初始化函数，用于注册命令行参数
func init() {
	rootCmd.PersistentFlags().
		StringVar(&configFile, "config", "", "配置文件路径 (默认是 $HOME/.mcp.json)")
	rootCmd.PersistentFlags().
		StringVar(&systemPromptFile, "system-prompt", "", "系统提示词 JSON 文件")
	rootCmd.PersistentFlags().
		IntVar(&messageWindow, "message-window", 10, "上下文中保留的消息数")

	// 模型选择参数，支持 anthropic/openai/ollama/google 等格式
	rootCmd.PersistentFlags().
		StringVarP(&modelFlag, "model", "m", "anthropic:claude-3-5-sonnet-latest",
			"使用的模型（格式：provider:model，例如 openai:gpt-4 或 ollama:qwen2.5:3b）")

	// 调试模式开关
	rootCmd.PersistentFlags().
		BoolVar(&debugMode, "debug", false, "启用调试日志")

	// 设置 API 参数
	flags := rootCmd.PersistentFlags()
	flags.StringVar(&openaiBaseURL, "openai-url", "", "OpenAI API 基础地址（默认是 api.openai.com）")
	flags.StringVar(&anthropicBaseURL, "anthropic-url", "", "Anthropic API 基础地址（默认是 api.anthropic.com）")
	flags.StringVar(&openaiAPIKey, "openai-api-key", "", "OpenAI API 密钥")
	flags.StringVar(&anthropicAPIKey, "anthropic-api-key", "", "Anthropic API 密钥")
	flags.StringVar(&googleAPIKey, "google-api-key", "", "Google Gemini API 密钥")
}

// 创建 AI Provider 实例，根据 --model 参数动态选择后端模型提供方
func createProvider(ctx context.Context, modelString, systemPrompt string) (llm.Provider, error) {
	// 模型参数格式必须为 "provider:model"，例如 "openai:gpt-4"
	parts := strings.SplitN(modelString, ":", 2)
	if len(parts) < 2 {
		return nil, fmt.Errorf("模型参数格式错误，应为 provider:model，实际收到 %s", modelString)
	}

	provider := parts[0] // 提供方，如 openai、ollama、google、anthropic
	model := parts[1]    // 模型名称

	switch provider {
	case "anthropic":
		// 获取 API Key，优先从命令行参数获取，其次从环境变量中获取
		apiKey := anthropicAPIKey
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("未提供 Anthropic API 密钥，请使用 --anthropic-api-key 或设置 ANTHROPIC_API_KEY 环境变量")
		}
		// 创建并返回 anthropic provider 实例
		return anthropic.NewProvider(apiKey, anthropicBaseURL, model, systemPrompt), nil

	case "ollama":
		// Ollama 本地模型不需要 API Key，直接返回
		return ollama.NewProvider(model, systemPrompt)

	case "openai":
		apiKey := openaiAPIKey
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("未提供 OpenAI API 密钥，请使用 --openai-api-key 或设置 OPENAI_API_KEY 环境变量")
		}
		return openai.NewProvider(apiKey, openaiBaseURL, model, systemPrompt), nil

	case "google":
		apiKey := googleAPIKey
		if apiKey == "" {
			apiKey = os.Getenv("GOOGLE_API_KEY") // 优先从 GOOGLE_API_KEY 获取
		}
		if apiKey == "" {
			apiKey = os.Getenv("GEMINI_API_KEY") // 兼容 Gemini 平台命名
		}
		return google.NewProvider(ctx, apiKey, model, systemPrompt)

	default:
		// 不支持的提供方
		return nil, fmt.Errorf("不支持的模型提供方: %s", provider)
	}
}

// pruneMessages 用于裁剪对话历史，保留最近的 messageWindow 条消息，并移除无效的工具调用和结果。
func pruneMessages(messages []history.HistoryMessage) []history.HistoryMessage {
	if len(messages) <= messageWindow {
		return messages // 如果消息数量没超过窗口限制，原样返回
	}

	// 仅保留最后 messageWindow 条消息
	messages = messages[len(messages)-messageWindow:]

	toolUseIds := make(map[string]bool)    // 用于记录有效的 tool_use ID
	toolResultIds := make(map[string]bool) // 用于记录有效的 tool_result 所引用的 tool_use ID

	// 第一次遍历：收集所有工具调用和结果的 ID
	for _, msg := range messages {
		for _, block := range msg.Content {
			if block.Type == "tool_use" {
				toolUseIds[block.ID] = true
			} else if block.Type == "tool_result" {
				toolResultIds[block.ToolUseID] = true
			}
		}
	}

	// 第二次遍历：只保留有关联的工具调用和结果
	var prunedMessages []history.HistoryMessage
	for _, msg := range messages {
		var prunedBlocks []history.ContentBlock
		for _, block := range msg.Content {
			keep := true
			if block.Type == "tool_use" {
				keep = toolResultIds[block.ID] // 仅保留被引用的 tool_use
			} else if block.Type == "tool_result" {
				keep = toolUseIds[block.ToolUseID] // 仅保留对应存在的 tool_result
			}
			if keep {
				prunedBlocks = append(prunedBlocks, block)
			}
		}

		// 仅保留有内容或非助手的消息
		if (len(prunedBlocks) > 0 && msg.Role == "assistant") || msg.Role != "assistant" {
			hasTextBlock := false
			for _, block := range msg.Content {
				if block.Type == "text" {
					hasTextBlock = true
					break
				}
			}
			if len(prunedBlocks) > 0 || hasTextBlock {
				msg.Content = prunedBlocks
				prunedMessages = append(prunedMessages, msg)
			}
		}
	}
	return prunedMessages
}

// 获取当前终端的宽度，返回值减去20以适配美观的输出宽度
func getTerminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return 80 // 默认宽度
	}
	return width - 20
}

// 显示历史消息内容
func handleHistoryCommand(messages []history.HistoryMessage) {
	displayMessageHistory(messages)
}

// 根据终端宽度更新 Markdown 渲染器
func updateRenderer() error {
	width := getTerminalWidth()
	var err error
	renderer, err = glamour.NewTermRenderer(
		glamour.WithStandardStyle(styles.TokyoNightStyle), // 使用主题样式
		glamour.WithWordWrap(width),                       // 设置自动换行
	)
	return err
}

// runPrompt 向 LLM 发送 prompt，并处理返回内容及可能的工具调用
func runPrompt(
	ctx context.Context,
	provider llm.Provider,
	mcpClients map[string]mcpclient.MCPClient,
	tools []llm.Tool,
	prompt string,
	messages *[]history.HistoryMessage,
) error {
	// 用户有 prompt 输入时，将其加入消息历史
	if prompt != "" {
		fmt.Printf("\n%s\n", promptStyle.Render("You: "+prompt))
		*messages = append(*messages, history.HistoryMessage{
			Role: "user",
			Content: []history.ContentBlock{{
				Type: "text",
				Text: prompt,
			}},
		})
	}

	var message llm.Message
	var err error
	backoff := initialBackoff
	retries := 0

	// 构建 llm 消息列表（接口适配）
	llmMessages := make([]llm.Message, len(*messages))
	for i := range *messages {
		llmMessages[i] = &(*messages)[i]
	}

	// 重试机制，直到获取响应或超过最大次数
	for {
		action := func() {
			message, err = provider.CreateMessage(
				ctx, prompt, llmMessages, tools)
		}
		_ = spinner.New().Title("Thinking...").Action(action).Run()

		if err != nil {
			if strings.Contains(err.Error(), "overloaded_error") {
				if retries >= maxRetries {
					return fmt.Errorf("claude 当前过载，请稍后重试")
				}
				log.Warn("Claude过载，退避重试",
					"attempt", retries+1,
					"backoff", backoff.String())
				time.Sleep(backoff)
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				retries++
				continue
			}
			return err
		}
		break
	}

	var messageContent []history.ContentBlock

	// 显示 LLM 返回内容
	if str, err := renderer.Render("\nAssistant: "); message.GetContent() != "" && err == nil {
		fmt.Print(str)
	}

	toolResults := []history.ContentBlock{}
	messageContent = []history.ContentBlock{}

	// 处理普通文本内容
	if message.GetContent() != "" {
		if err := updateRenderer(); err != nil {
			return fmt.Errorf("更新渲染器失败: %v", err)
		}
		str, err := renderer.Render(message.GetContent() + "\n")
		if err != nil {
			log.Error("响应渲染失败", "error", err)
			fmt.Print(message.GetContent() + "\n")
		} else {
			fmt.Print(str)
		}
		messageContent = append(messageContent, history.ContentBlock{
			Type: "text",
			Text: message.GetContent(),
		})
	}

	// 处理工具调用
	for _, toolCall := range message.GetToolCalls() {
		log.Info("🔧 调用工具", "name", toolCall.GetName())

		input, _ := json.Marshal(toolCall.GetArguments())
		messageContent = append(messageContent, history.ContentBlock{
			Type:  "tool_use",
			ID:    toolCall.GetID(),
			Name:  toolCall.GetName(),
			Input: input,
		})

		inputTokens, outputTokens := message.GetUsage()
		if inputTokens > 0 || outputTokens > 0 {
			log.Info("令牌使用情况", "input", inputTokens,
				"output", outputTokens, "total", inputTokens+outputTokens)
		}

		// 工具名称格式：server__tool
		parts := strings.Split(toolCall.GetName(), "__")
		if len(parts) != 2 {
			fmt.Printf("错误：无效工具名称：%s\n", toolCall.GetName())
			continue
		}

		serverName, toolName := parts[0], parts[1]
		mcpClient, ok := mcpClients[serverName]
		if !ok {
			fmt.Printf("错误：找不到服务器：%s\n", serverName)
			continue
		}

		var toolArgs map[string]interface{}
		if err := json.Unmarshal(input, &toolArgs); err != nil {
			fmt.Printf("解析工具参数失败: %v\n", err)
			continue
		}

		var toolResultPtr *mcp.CallToolResult
		action := func() {
			req := mcp.CallToolRequest{}
			req.Params.Name = toolName
			req.Params.Arguments = toolArgs
			toolResultPtr, err = mcpClient.CallTool(context.Background(), req)
		}
		_ = spinner.New().
			Title(fmt.Sprintf("运行工具 %s...", toolName)).
			Action(action).
			Run()

		if err != nil {
			errMsg := fmt.Sprintf("调用工具 %s 错误: %v", toolName, err)
			fmt.Printf("\n%s\n", errorStyle.Render(errMsg))
			toolResults = append(toolResults, history.ContentBlock{
				Type:      "tool_result",
				ToolUseID: toolCall.GetID(),
				Content: []history.ContentBlock{{
					Type: "text",
					Text: errMsg,
				}},
			})
			continue
		}

		toolResult := *toolResultPtr
		if toolResult.Content != nil {
			log.Debug("工具结果内容", "content", toolResult.Content)

			resultBlock := history.ContentBlock{
				Type:      "tool_result",
				ToolUseID: toolCall.GetID(),
				Content:   toolResult.Content,
			}

			var resultText string
			for _, item := range toolResult.Content {
				if contentMap, ok := item.(mcp.TextContent); ok {
					resultText += fmt.Sprintf("%v ", contentMap.Text)
				}
			}

			resultBlock.Text = strings.TrimSpace(resultText)
			log.Debug("构建工具结果块", "block", resultBlock)
			toolResults = append(toolResults, resultBlock)
		}
	}

	// 添加助手消息（包含文本 + 工具调用）
	*messages = append(*messages, history.HistoryMessage{
		Role:    message.GetRole(),
		Content: messageContent,
	})

	// 如果存在工具结果，则添加并再次调用 LLM
	if len(toolResults) > 0 {
		for _, toolResult := range toolResults {
			*messages = append(*messages, history.HistoryMessage{
				Role:    "tool",
				Content: []history.ContentBlock{toolResult},
			})
		}
		// 继续对工具结果进行回复处理
		return runPrompt(ctx, provider, mcpClients, tools, "", messages)
	}

	fmt.Println() // 输出空行以分隔
	return nil
}

// runMCPHost 启动 MCP 主机，设置日志、加载配置并启动交互循环
func runMCPHost(ctx context.Context) error {
	// 根据调试模式设置日志级别
	if debugMode {
		log.SetLevel(log.DebugLevel) // 设置为调试级别
		log.SetReportCaller(true)    // 启用日志中的调用者信息
	} else {
		log.SetLevel(log.InfoLevel) // 设置为信息级别
		log.SetReportCaller(false)  // 禁用调用者信息
	}

	// 加载系统提示语
	systemPrompt, err := loadSystemPrompt(systemPromptFile)
	if err != nil {
		return fmt.Errorf("加载系统提示失败: %v", err)
	}

	// 创建 LLM 提供者（根据模型标志选择）
	fmt.Println("开始创建 provider ")
	provider, err := createProvider(ctx, modelFlag, systemPrompt)
	if err != nil {
		return fmt.Errorf("创建提供者失败: %v", err)
	}

	// 从模型标志中分离出模型名称
	parts := strings.SplitN(modelFlag, ":", 2)
	log.Info("模型加载成功",
		"provider", provider.Name(),
		"model", parts[1])

	// 加载 MCP 配置
	fmt.Println("开始加载 MCP 配置")
	mcpConfig, err := loadMCPConfig()
	if err != nil {
		return fmt.Errorf("加载 MCP 配置失败: %v", err)
	}

	// 创建 MCP 客户端
	mcpClients, err := createMCPClients(mcpConfig)
	n := len(mcpClients)
	if n == 0 {
		fmt.Println("没有 mcpClients ")
	}
	if err != nil {
		return fmt.Errorf("创建 MCP 客户端失败: %v", err)
	}

	// 确保在函数退出时关闭所有 MCP 客户端
	defer func() {
		log.Info("正在关闭 MCP 服务器...")
		for name, client := range mcpClients {
			if err := client.Close(); err != nil {
				log.Error("关闭服务器失败", "name", name, "error", err)
			} else {
				log.Info("服务器已关闭", "name", name)
			}
		}
	}()

	// 打印每个已连接的服务器
	for name := range mcpClients {
		log.Info("服务器已连接", "name", name)
	}

	// 收集所有工具
	var allTools []llm.Tool
	for serverName, mcpClient := range mcpClients {
		// 设置 10 秒的超时
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		// 获取工具列表
		toolsResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
		cancel()

		if err != nil {
			log.Error(
				"获取工具失败",
				"server", serverName,
				"error", err,
			)
			continue
		}

		// 将工具转换为支持的格式
		serverTools := mcpToolsToAnthropicTools(serverName, toolsResult.Tools)
		allTools = append(allTools, serverTools...)
		log.Info(
			"工具加载成功",
			"server", serverName,
			"count", len(toolsResult.Tools),
		)
	}

	// 初始化渲染器
	if err := updateRenderer(); err != nil {
		return fmt.Errorf("初始化渲染器失败: %v", err)
	}

	// 用于存储消息历史
	messages := make([]history.HistoryMessage, 0)

	// 主交互循环
	for {
		// 获取用户输入的提示
		var prompt string
		err := huh.NewForm(huh.NewGroup(huh.NewText().
			Title("请输入提示语 (输入 /help 查看命令，Ctrl+C 退出)").
			Value(&prompt).
			CharLimit(5000)),
		).WithWidth(getTerminalWidth()).
			WithTheme(huh.ThemeCharm()).
			Run()

		if err != nil {
			// 如果是用户中止（Ctrl+C）
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Println("\n再见！")
				return nil // 正常退出
			}
			return err // 其他错误直接返回
		}

		// 如果用户没有输入任何内容，则继续循环
		if prompt == "" {
			continue
		}

		// 处理斜杠命令（如 /help 等）
		handled, err := handleSlashCommand(
			prompt,
			mcpConfig,
			mcpClients,
			messages,
		)
		if err != nil {
			return err
		}
		if handled {
			continue // 如果是命令处理过，则跳过后续操作
		}

		// 如果消息历史不为空，则清理过期的消息
		if len(messages) > 0 {
			messages = pruneMessages(messages)
		}

		// 调用模型生成回复
		err = runPrompt(ctx, provider, mcpClients, allTools, prompt, &messages)
		if err != nil {
			return err
		}
	}
}

// loadSystemPrompt 从文件加载系统提示
func loadSystemPrompt(filePath string) (string, error) {
	// 如果没有提供文件路径，则返回空字符串
	if filePath == "" {
		return "", nil
	}

	// 读取文件内容
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("读取配置文件失败: %v", err)
	}

	// 解析文件内容中的 systemPrompt 字段
	var config struct {
		SystemPrompt string `json:"systemPrompt"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("解析配置文件失败: %v", err)
	}

	return config.SystemPrompt, nil
}
