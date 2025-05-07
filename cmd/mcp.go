package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/huh/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/list"
	"github.com/charmbracelet/log"

	"strings"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcphost/pkg/history"
	"github.com/mark3labs/mcphost/pkg/llm"
)

const (
	transportStdio = "stdio"
	transportSSE   = "sse"
)

var (
	// Tokyo Night 主题配色（使用 ANSI 色码）
	tokyoPurple = lipgloss.Color("99")  // 紫色 #9d7cd8
	tokyoCyan   = lipgloss.Color("73")  // 青色 #7dcfff
	tokyoBlue   = lipgloss.Color("111") // 蓝色 #7aa2f7
	tokyoGreen  = lipgloss.Color("120") // 绿色 #73daca
	tokyoRed    = lipgloss.Color("203") // 红色 #f7768e
	tokyoOrange = lipgloss.Color("215") // 橙色 #ff9e64
	tokyoFg     = lipgloss.Color("189") // 前景色 #c0caf5
	tokyoGray   = lipgloss.Color("237") // 灰色 #3b4261
	tokyoBg     = lipgloss.Color("234") // 背景色 #1a1b26

	// 提示样式：蓝色文字，左边填充 2 空格
	promptStyle = lipgloss.NewStyle().
			Foreground(tokyoBlue).
			PaddingLeft(2)

	// 响应样式：前景色为亮色，左边填充 2 空格
	responseStyle = lipgloss.NewStyle().
			Foreground(tokyoFg).
			PaddingLeft(2)

	// 错误样式：红色文字，加粗
	errorStyle = lipgloss.NewStyle().
			Foreground(tokyoRed).
			Bold(true)

	// 工具名样式：青色文字，加粗
	toolNameStyle = lipgloss.NewStyle().
			Foreground(tokyoCyan).
			Bold(true)

	// 描述样式：前景为亮色，下边距 1 行
	descriptionStyle = lipgloss.NewStyle().
				Foreground(tokyoFg).
				PaddingBottom(1)

	// 内容样式：背景为暗色，两侧填充 4 空格
	contentStyle = lipgloss.NewStyle().
			Background(tokyoBg).
			PaddingLeft(4).
			PaddingRight(4)
)

// MCPConfig 定义了 MCP 服务器配置结构体
type MCPConfig struct {
	MCPServers map[string]ServerConfigWrapper `json:"mcpServers"`
}

// ServerConfig 接口，表示服务器配置的统一接口
type ServerConfig interface {
	GetType() string
}

// STDIOServerConfig 表示本地命令行执行的服务器配置
type STDIOServerConfig struct {
	Command string            `json:"command"`       // 执行的命令
	Args    []string          `json:"args"`          // 命令参数
	Env     map[string]string `json:"env,omitempty"` // 可选的环境变量
}

// GetType 返回 STDIOServerConfig 的类型标识
func (s STDIOServerConfig) GetType() string {
	return transportStdio
}

// SSEServerConfig 表示 SSE 协议的远程服务器配置
type SSEServerConfig struct {
	Url     string   `json:"url"`               // SSE 服务器的 URL
	Headers []string `json:"headers,omitempty"` // 可选的请求头
}

// GetType 返回 SSEServerConfig 的类型标识
func (s SSEServerConfig) GetType() string {
	return transportSSE
}

// ServerConfigWrapper 是一个包装类型，用于支持动态解析两种类型的配置
type ServerConfigWrapper struct {
	Config ServerConfig // 实际存储的是接口类型，可以是 SSE 或 STDIO
}

// UnmarshalJSON 自定义反序列化逻辑，根据字段判断是哪个配置类型
func (w *ServerConfigWrapper) UnmarshalJSON(data []byte) error {
	var typeField struct {
		Url string `json:"url"`
	}

	// 先尝试解析是否存在 url 字段
	if err := json.Unmarshal(data, &typeField); err != nil {
		return err
	}

	if typeField.Url != "" {
		// 存在 url 字段 -> SSE 类型
		var sse SSEServerConfig
		if err := json.Unmarshal(data, &sse); err != nil {
			return err
		}
		w.Config = sse
	} else {
		// 否则为 STDIO 类型
		var stdio STDIOServerConfig
		if err := json.Unmarshal(data, &stdio); err != nil {
			return err
		}
		w.Config = stdio
	}

	return nil
}

// MarshalJSON 将包装的 Config 接口序列化为 JSON
func (w ServerConfigWrapper) MarshalJSON() ([]byte, error) {
	return json.Marshal(w.Config)
}

// mcpToolsToAnthropicTools 将 MCP 工具转换为 Anthropic 兼容的工具格式
func mcpToolsToAnthropicTools(
	serverName string, // 所属服务器名
	mcpTools []mcp.Tool, // 原始 MCP 工具列表
) []llm.Tool {
	anthropicTools := make([]llm.Tool, len(mcpTools)) // 初始化返回切片

	for i, tool := range mcpTools {
		// 工具名添加命名空间前缀，避免冲突
		namespacedName := fmt.Sprintf("%s__%s", serverName, tool.Name)

		// 构造新的工具对象
		anthropicTools[i] = llm.Tool{
			Name:        namespacedName,
			Description: tool.Description,
			InputSchema: llm.Schema{
				Type:       tool.InputSchema.Type,
				Properties: tool.InputSchema.Properties,
				Required:   tool.InputSchema.Required,
			},
		}
	}

	return anthropicTools
}

// 加载 MCP 配置文件（优先使用 configFile，否则默认从 ~/.mcp.json 加载）
func loadMCPConfig() (*MCPConfig, error) {
	var configPath string
	if configFile != "" {
		// 如果有传入配置路径，使用该路径
		configPath = configFile
	} else {
		// 否则获取用户主目录下的 .mcp.json 文件
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("获取用户主目录失败: %w", err)
		}
		configPath = filepath.Join(homeDir, ".mcp.json")
	}

	// 如果配置文件不存在，则创建默认配置文件
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := MCPConfig{
			MCPServers: make(map[string]ServerConfigWrapper),
		}

		// 序列化默认配置为 JSON
		configData, err := json.MarshalIndent(defaultConfig, "", "  ")
		if err != nil {
			return nil, fmt.Errorf("创建默认配置失败: %w", err)
		}

		// 写入默认配置文件
		if err := os.WriteFile(configPath, configData, 0644); err != nil {
			return nil, fmt.Errorf("写入默认配置文件失败: %w", err)
		}

		log.Info("已创建默认配置文件", "path", configPath)
		return &defaultConfig, nil
	}

	// 读取已有配置文件内容
	configData, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败 %s: %w", configPath, err)
	}

	var config MCPConfig
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &config, nil
}

// 根据配置创建所有 MCP 客户端（支持 SSE 和 STDIO 类型）
func createMCPClients(config *MCPConfig) (map[string]mcpclient.MCPClient, error) {
	clients := make(map[string]mcpclient.MCPClient)

	for name, server := range config.MCPServers {
		var client mcpclient.MCPClient
		var err error

		if server.Config.GetType() == transportSSE {
			// 处理 SSE 类型的服务
			sseConfig := server.Config.(SSEServerConfig)
			options := []mcpclient.ClientOption{}

			if sseConfig.Headers != nil {
				headers := make(map[string]string)
				// 解析 headers（例如 "Authorization: Bearer xxx"）
				for _, header := range sseConfig.Headers {
					parts := strings.SplitN(header, ":", 2)
					if len(parts) == 2 {
						key := strings.TrimSpace(parts[0])
						value := strings.TrimSpace(parts[1])
						headers[key] = value
					}
				}
				options = append(options, mcpclient.WithHeaders(headers))
			}

			// 创建 SSE 客户端并启动
			client, err = mcpclient.NewSSEMCPClient(sseConfig.Url, options...)
			if err == nil {
				err = client.(*mcpclient.SSEMCPClient).Start(context.Background())
			}
		} else {
			// 处理 STDIO 类型的服务（本地子进程）
			stdioConfig := server.Config.(STDIOServerConfig)
			var env []string
			for k, v := range stdioConfig.Env {
				env = append(env, fmt.Sprintf("%s=%s", k, v))
			}
			client, err = mcpclient.NewStdioMCPClient(stdioConfig.Command, env, stdioConfig.Args...)
		}

		if err != nil {
			// 出现错误则关闭所有已创建的客户端并返回
			for _, c := range clients {
				c.Close()
			}
			return nil, fmt.Errorf("创建 MCP 客户端失败（%s）: %w", name, err)
		}

		// 初始化客户端
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		log.Info("正在初始化服务...", "name", name)
		initRequest := mcp.InitializeRequest{}
		initRequest.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
		initRequest.Params.ClientInfo = mcp.Implementation{
			Name:    "mcphost",
			Version: "0.1.0",
		}
		initRequest.Params.Capabilities = mcp.ClientCapabilities{}

		_, err = client.Initialize(ctx, initRequest)
		if err != nil {
			client.Close()
			for _, c := range clients {
				c.Close()
			}
			return nil, fmt.Errorf("初始化 MCP 客户端失败（%s）: %w", name, err)
		}

		// 加入返回列表
		clients[name] = client
	}

	return clients, nil
}

// 处理用户输入的命令（以 "/" 开头）
func handleSlashCommand(
	prompt string,
	mcpConfig *MCPConfig,
	mcpClients map[string]mcpclient.MCPClient,
	messages interface{},
) (bool, error) {
	if !strings.HasPrefix(prompt, "/") {
		// 不是命令，正常处理
		return false, nil
	}

	switch strings.ToLower(strings.TrimSpace(prompt)) {
	case "/tools":
		handleToolsCommand(mcpClients)
		return true, nil
	case "/help":
		handleHelpCommand()
		return true, nil
	case "/history":
		handleHistoryCommand(messages.([]history.HistoryMessage))
		return true, nil
	case "/servers":
		handleServersCommand(mcpConfig)
		return true, nil
	case "/quit":
		fmt.Println("\nGoodbye!")
		defer os.Exit(0) // 延迟退出，确保输出完成
		return true, nil
	default:
		fmt.Printf("%s\nType /help to see available commands\n\n",
			errorStyle.Render("Unknown command: "+prompt))
		return true, nil
	}
}

// 展示帮助信息
func handleHelpCommand() {
	if err := updateRenderer(); err != nil {
		fmt.Printf("\n%s\n", errorStyle.Render(fmt.Sprintf("更新渲染器失败: %v", err)))
		return
	}

	var markdown strings.Builder

	markdown.WriteString("# 可用命令\n\n")
	markdown.WriteString("你可以使用以下命令：\n\n")
	markdown.WriteString("- **/help**: 显示此帮助信息\n")
	markdown.WriteString("- **/tools**: 列出所有可用工具\n")
	markdown.WriteString("- **/servers**: 列出已配置的 MCP 服务器\n")
	markdown.WriteString("- **/history**: 显示会话历史记录\n")
	markdown.WriteString("- **/quit**: 退出程序\n")
	markdown.WriteString("\n你也可以随时按下 Ctrl+C 退出程序。\n")

	markdown.WriteString("\n## 支持的模型\n\n")
	markdown.WriteString("通过 --model 或 -m 参数指定模型，例如：\n\n")
	markdown.WriteString("- **Anthropic Claude**: `anthropic:claude-3-5-sonnet-latest`\n")
	markdown.WriteString("- **Ollama 模型**: `ollama:modelname`\n")
	markdown.WriteString("\n示例用法：\n")
	markdown.WriteString("```\n")
	markdown.WriteString("mcphost -m anthropic:claude-3-5-sonnet-latest\n")
	markdown.WriteString("mcphost -m ollama:qwen2.5:3b\n")
	markdown.WriteString("```\n")

	rendered, err := renderer.Render(markdown.String())
	if err != nil {
		fmt.Printf("\n%s\n", errorStyle.Render(fmt.Sprintf("渲染帮助信息失败: %v", err)))
		return
	}

	fmt.Print(rendered)
}

// 处理 `servers` 命令，展示所有配置的服务信息
func handleServersCommand(config *MCPConfig) {
	// 更新渲染器，如果失败则打印错误信息并退出
	if err := updateRenderer(); err != nil {
		fmt.Printf(
			"\n%s\n",
			errorStyle.Render(fmt.Sprintf("Error updating renderer: %v", err)),
		)
		return
	}

	var markdown strings.Builder // 构造 markdown 文本
	action := func() {
		if len(config.MCPServers) == 0 {
			markdown.WriteString("No servers configured.\n")
		} else {
			for name, server := range config.MCPServers {
				markdown.WriteString(fmt.Sprintf("# %s\n\n", name))

				if server.Config.GetType() == transportSSE {
					// SSE 类型服务器配置
					sseConfig := server.Config.(SSEServerConfig)
					markdown.WriteString("*Url*\n")
					markdown.WriteString(fmt.Sprintf("`%s`\n\n", sseConfig.Url))
					markdown.WriteString("*headers*\n")
					if sseConfig.Headers != nil {
						for _, header := range sseConfig.Headers {
							// 仅显示 header 的键名，值隐藏
							parts := strings.SplitN(header, ":", 2)
							if len(parts) == 2 {
								key := strings.TrimSpace(parts[0])
								markdown.WriteString("`" + key + ": [REDACTED]`\n")
							}
						}
					} else {
						markdown.WriteString("*None*\n")
					}
				} else {
					// STDIO 类型服务器配置
					stdioConfig := server.Config.(STDIOServerConfig)
					markdown.WriteString("*Command*\n")
					markdown.WriteString(fmt.Sprintf("`%s`\n\n", stdioConfig.Command))

					markdown.WriteString("*Arguments*\n")
					if len(stdioConfig.Args) > 0 {
						markdown.WriteString(fmt.Sprintf("`%s`\n", strings.Join(stdioConfig.Args, " ")))
					} else {
						markdown.WriteString("*None*\n")
					}
				}
				markdown.WriteString("\n") // 每个服务器之间加空行
			}
		}
	}

	// 使用 spinner 动画加载信息
	_ = spinner.New().
		Title("Loading server configuration...").
		Action(action).
		Run()

	// 渲染 markdown 到终端样式
	rendered, err := renderer.Render(markdown.String())
	if err != nil {
		fmt.Printf(
			"\n%s\n",
			errorStyle.Render(fmt.Sprintf("Error rendering servers: %v", err)),
		)
		return
	}

	// 设置容器样式（左右边距）
	containerStyle := lipgloss.NewStyle().
		MarginLeft(4).
		MarginRight(4)

	// 打印最终渲染结果
	fmt.Print("\n" + containerStyle.Render(rendered) + "\n")
}

// 处理 `tools` 命令，展示所有服务器支持的工具
func handleToolsCommand(mcpClients map[string]mcpclient.MCPClient) {
	width := getTerminalWidth() // 获取终端宽度
	contentWidth := width - 12  // 内容宽度，减去边距和标号

	// 若没有可用客户端，打印提示信息
	if len(mcpClients) == 0 {
		fmt.Print(
			"\n" + contentStyle.Render("Tools are currently disabled for this model.\n") + "\n\n",
		)
		return
	}

	// 用于存储每个服务器的工具及可能的错误
	type serverTools struct {
		tools []mcp.Tool
		err   error
	}
	results := make(map[string]serverTools)

	// 执行实际获取工具的操作
	action := func() {
		for serverName, mcpClient := range mcpClients {
			// 设置超时时间为 10 秒
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			toolsResult, err := mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
			if err != nil {
				// 如果请求失败，记录错误
				results[serverName] = serverTools{
					tools: nil,
					err:   err,
				}
				continue
			}

			var tools []mcp.Tool
			if toolsResult != nil {
				tools = toolsResult.Tools
			}

			// 保存获取到的工具
			results[serverName] = serverTools{
				tools: tools,
				err:   nil,
			}
		}
	}

	// 使用 spinner 显示加载中
	_ = spinner.New().
		Title("Fetching tools from all servers...").
		Action(action).
		Run()

	// 构建最终的嵌套列表
	l := list.New().
		EnumeratorStyle(lipgloss.NewStyle().Foreground(tokyoPurple).MarginRight(1))

	for serverName, result := range results {
		if result.err != nil {
			// 若出错则打印错误提示
			fmt.Printf(
				"\n%s\n",
				errorStyle.Render(
					fmt.Sprintf("Error fetching tools from %s: %v", serverName, result.err),
				),
			)
			continue
		}

		// 创建当前服务器的工具列表
		serverList := list.New().
			EnumeratorStyle(lipgloss.NewStyle().Foreground(tokyoCyan).MarginRight(1))

		if len(result.tools) == 0 {
			serverList.Item("No tools available")
		} else {
			for _, tool := range result.tools {
				// 工具描述样式，支持换行
				descStyle := lipgloss.NewStyle().
					Foreground(tokyoFg).
					Width(contentWidth).
					Align(lipgloss.Left)

				// 描述以子列表呈现
				toolDesc := list.New().
					EnumeratorStyle(lipgloss.NewStyle().Foreground(tokyoGreen).MarginRight(1)).
					Item(descStyle.Render(tool.Description))

				// 添加工具名及描述到列表
				serverList.Item(toolNameStyle.Render(tool.Name)).Item(toolDesc)
			}
		}

		// 将该服务器的工具添加到主列表中
		l.Item(serverName).Item(serverList)
	}

	// 设置整个列表的容器样式
	containerStyle := lipgloss.NewStyle().
		Margin(2).
		Width(width)

	// 打印最终渲染结果
	fmt.Print("\n" + containerStyle.Render(l.String()) + "\n")
}

// displayMessageHistory 函数用于展示历史对话消息内容（支持用户、助手、系统等角色的文本消息、工具调用与工具结果等类型）。
func displayMessageHistory(messages []history.HistoryMessage) {
	// 尝试更新渲染器，若失败则打印错误信息并返回
	if err := updateRenderer(); err != nil {
		fmt.Printf(
			"\n%s\n",
			errorStyle.Render(fmt.Sprintf("Error updating renderer: %v", err)),
		)
		return
	}

	// 创建 Markdown 字符串构建器，用于组装展示内容
	var markdown strings.Builder
	markdown.WriteString("# Conversation History\n\n") // 添加标题“对话历史”

	// 遍历每一条历史消息
	for _, msg := range messages {
		// 根据角色设置标题
		roleTitle := "## User"
		if msg.Role == "assistant" {
			roleTitle = "## Assistant"
		} else if msg.Role == "system" {
			roleTitle = "## System"
		}
		markdown.WriteString(roleTitle + "\n\n") // 添加角色标题

		// 遍历该消息下的每个内容块（ContentBlock）
		for _, block := range msg.Content {
			switch block.Type {
			case "text": // 普通文本消息
				markdown.WriteString("### Text\n")        // 子标题
				markdown.WriteString(block.Text + "\n\n") // 添加文本内容

			case "tool_use": // 工具调用消息
				markdown.WriteString("### Tool Use\n")
				markdown.WriteString(
					fmt.Sprintf("**Tool:** %s\n\n", block.Name), // 工具名称
				)
				// 如果存在输入参数，格式化为 JSON 并输出
				if block.Input != nil {
					prettyInput, err := json.MarshalIndent(
						block.Input,
						"",
						"  ",
					)
					if err != nil {
						markdown.WriteString(
							fmt.Sprintf("Error formatting input: %v\n\n", err),
						)
					} else {
						markdown.WriteString("**Input:**\n```json\n")
						markdown.WriteString(string(prettyInput))
						markdown.WriteString("\n```\n\n")
					}
				}

			case "tool_result": // 工具执行结果
				markdown.WriteString("### Tool Result\n")
				markdown.WriteString(
					fmt.Sprintf("**Tool ID:** %s\n\n", block.ToolUseID), // 工具 ID
				)
				// 支持结果是字符串或多个文本内容块
				switch v := block.Content.(type) {
				case string: // 如果是字符串直接输出
					markdown.WriteString("```\n")
					markdown.WriteString(v)
					markdown.WriteString("\n```\n\n")
				case []history.ContentBlock: // 如果是多个文本块，遍历并输出
					for _, contentBlock := range v {
						if contentBlock.Type == "text" {
							markdown.WriteString("```\n")
							markdown.WriteString(contentBlock.Text)
							markdown.WriteString("\n```\n\n")
						}
					}
				}
			}
		}
		// 每条消息之间添加分隔线
		markdown.WriteString("---\n\n")
	}

	// 将 Markdown 内容渲染为终端样式
	rendered, err := renderer.Render(markdown.String())
	if err != nil {
		fmt.Printf(
			"\n%s\n",
			errorStyle.Render(fmt.Sprintf("Error rendering history: %v", err)),
		)
		return
	}

	// 打印渲染结果到终端，不使用外框
	fmt.Print("\n" + rendered + "\n")
}
