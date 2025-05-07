package main

import (
	"github.com/mark3labs/mcphost/cmd"
	"os"
)

func main() {
	os.Args = append(os.Args, "-m", "ollama:qwen3:1.7b")
	//os.Args = append(os.Args, "-m", "google:gemini-2.0-flash")
	cmd.Execute()
}
