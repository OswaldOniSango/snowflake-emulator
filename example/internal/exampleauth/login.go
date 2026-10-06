// Package exampleauth supplies local demonstration authentication for examples.
package exampleauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// Login returns a token for ADMIN/admin, or SNOWFLAKE_USER/SNOWFLAKE_PASSWORD.
func Login(baseURL string) (string, error) {
	username, password := os.Getenv("SNOWFLAKE_USER"), os.Getenv("SNOWFLAKE_PASSWORD")
	if username == "" {
		username = "ADMIN"
	}
	if password == "" {
		password = "admin"
	}
	body, err := json.Marshal(map[string]any{"data": map[string]string{"LOGIN_NAME": username, "PASSWORD": password}})
	if err != nil {
		return "", err
	}
	response, err := http.Post(baseURL+"/session/v1/login-request", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var result struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", err
	}
	if response.StatusCode != http.StatusOK || !result.Success || result.Data.Token == "" {
		return "", fmt.Errorf("login failed: %s", result.Message)
	}
	return result.Data.Token, nil
}
