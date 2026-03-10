package main

import (
	"context"
	"fmt"
	"os"

	errly "github.com/AsYourWish-ai/Errly/sdk/go"
)

func main() {
	apiKey := os.Getenv("ERRLY_API_KEY")
	if apiKey == "" {
		fmt.Println("ERRLY_API_KEY is not set")
		os.Exit(1)
	}

	client := errly.New(
		"http://localhost:5080",
		apiKey,
		errly.WithProject("go-sdk-test"),
		errly.WithEnvironment("test"),
	)
	defer client.Flush()

	id := client.CaptureError(context.Background(), fmt.Errorf("SDK test error from Go"))
	fmt.Println("captured event:", id)

	client.CaptureMessage(context.Background(), "info", "Go SDK integration test passed")
	fmt.Println("Go SDK test complete")
}
