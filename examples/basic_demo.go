package main

import (
	"context"
	"fmt"
	"log"
	"os"

	republic "github.com/seanly/dmr-devkit/republic"
)

func main() {
	// 从环境变量读取配置
	apiKey := os.Getenv("AI_API_KEY")
	apiBase := os.Getenv("AI_API_BASE")
	model := os.Getenv("AI_MODEL")

	if apiKey == "" {
		log.Fatal("AI_API_KEY environment variable is required")
	}
	if model == "" {
		model = "gemini-3.1-flash-lite-preview" // 默认模型
	}

	fmt.Printf("🚀 Testing DMR with:\n")
	fmt.Printf("   Model: %s\n", model)
	fmt.Printf("   Base URL: %s\n", apiBase)
	fmt.Printf("   API Key: %s...\n\n", apiKey[:10])

	// 创建 LLM 实例
	llm := republic.New(republic.Config{
		Model:      model,
		APIKey:     apiKey,
		BaseURL:    apiBase,
		MaxRetries: 2,
		Verbose:    1,
	})

	ctx := context.Background()

	// 测试 1: 简单对话
	fmt.Println("📝 Test 1: Simple Chat")
	fmt.Println("Prompt: Describe DMR in one sentence.")
	response, err := llm.Chat(ctx, "Describe DMR (Decision-Making Republic) in one sentence.")
	if err != nil {
		log.Fatalf("❌ Chat failed: %v", err)
	}
	fmt.Printf("✅ Response: %s\n\n", response)

	// 测试 2: 流式输出
	fmt.Println("📝 Test 2: Streaming Chat")
	fmt.Println("Prompt: Tell me a short joke about programming.")
	stream, state, err := llm.Stream(ctx, "Tell me a short joke about programming.")
	if err != nil {
		log.Fatalf("❌ Stream failed: %v", err)
	}
	fmt.Print("✅ Response: ")
	for chunk := range stream {
		fmt.Print(chunk)
	}
	fmt.Println()
	if state.Usage != nil {
		fmt.Printf("   Usage: %v tokens\n", state.Usage["total_tokens"])
	}
	fmt.Println()

	// 测试 3: If 判断
	fmt.Println("📝 Test 3: If Decision")
	fmt.Println("Input: The server CPU is at 95%")
	fmt.Println("Question: Should we scale up?")
	decision, err := llm.If(ctx, "The server CPU is at 95%", "Should we scale up?")
	if err != nil {
		log.Fatalf("❌ If failed: %v", err)
	}
	fmt.Printf("✅ Decision: %v\n\n", decision)

	// 测试 4: Classify 分类
	fmt.Println("📝 Test 4: Classification")
	fmt.Println("Input: I need help with my invoice")
	fmt.Println("Choices: [support, sales, billing]")
	label, err := llm.Classify(ctx, "I need help with my invoice", []string{"support", "sales", "billing"})
	if err != nil {
		log.Fatalf("❌ Classify failed: %v", err)
	}
	fmt.Printf("✅ Classification: %s\n\n", label)

	fmt.Println("🎉 All tests passed! Your configuration is working correctly.")
}
