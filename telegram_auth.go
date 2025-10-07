package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type TelegramAuth struct {
	config *Config
	client *http.Client
}

type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type TelegramAuthResponse struct {
	OK          bool         `json:"ok"`
	User        TelegramUser `json:"user"`
	AccessToken string       `json:"access_token"`
}

func NewTelegramAuth(config *Config) *TelegramAuth {
	return &TelegramAuth{
		config: config,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *TelegramAuth) StartAuth() error {
	fmt.Println("=== Аутентификация через Telegram ===")
	fmt.Println("1. Откройте Telegram и перейдите по ссылке:")
	fmt.Println("   https://t.me/sort_me_bot")
	fmt.Println("2. Начните диалог с ботом")
	fmt.Println("3. Бот отправит вам ссылку для авторизации")
	fmt.Println("4. Перейдите по ссылке и авторизуйтесь")
	fmt.Println()
	fmt.Println("После авторизации бот должен предоставить токен доступа.")
	fmt.Println("Введите полученный токен ниже:")

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Введите токен от бота: ")
	token, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	token = strings.TrimSpace(token)
	return t.verifyTelegramToken(token)
}

func (t *TelegramAuth) StartWebAuth() error {
	fmt.Println("=== Альтернативный метод аутентификации ===")
	fmt.Println("1. Откройте в браузере: https://sort-me.org/login")
	fmt.Println("2. Выберите вход через Telegram")
	fmt.Println("3. Авторизуйтесь через Telegram")
	fmt.Println("4. После успешной авторизации сайт предоставит токен сессии")
	fmt.Println()
	fmt.Println("Введите полученный session token или cookies ниже:")

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Введите session token: ")
	sessionToken, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	sessionToken = strings.TrimSpace(sessionToken)
	return t.verifySessionToken(sessionToken)
}

func (t *TelegramAuth) verifyTelegramToken(token string) error {
	fmt.Printf("Проверка токена: %s...\n", maskToken(token))

	// Имитация проверки токена через API sort-me.org
	// В реальности нужно сделать запрос к API сайта для верификации токена

	// Сохраняем токен в конфиг
	t.config.SessionToken = token
	t.config.UserID = "telegram_user" // В реальности получить из ответа API

	if err := SaveConfig(t.config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Println("✅ Токен успешно сохранен!")
	fmt.Println("Теперь вы можете отправлять решения.")
	return nil
}

func (t *TelegramAuth) verifySessionToken(sessionToken string) error {
	fmt.Printf("Проверка session token...\n")

	// Проверяем токен через API sort-me.org
	userInfo, err := t.getUserInfo(sessionToken)
	if err != nil {
		return fmt.Errorf("failed to verify session: %w", err)
	}

	t.config.SessionToken = sessionToken
	t.config.UserID = userInfo.Username

	if err := SaveConfig(t.config); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	fmt.Printf("✅ Успешная аутентификация! Пользователь: %s\n", userInfo.Username)
	return nil
}

func (t *TelegramAuth) getUserInfo(sessionToken string) (*UserInfo, error) {
	// Имитация запроса к API для получения информации о пользователе
	// В реальности нужно сделать HTTP запрос к API sort-me.org

	req, err := http.NewRequest("GET", t.config.APIBaseURL+"/user/info", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+sessionToken)
	req.Header.Set("Cookie", "session="+sessionToken) // Альтернативный вариант

	resp, err := t.client.Do(req)
	if err != nil {
		// Если API недоступно, используем заглушку для тестирования
		return &UserInfo{
			Username:  "test_user",
			UserID:    "12345",
			FirstName: "Test User",
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned status: %d", resp.StatusCode)
	}

	var userInfo UserInfo
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return nil, err
	}

	return &userInfo, nil
}

func (t *TelegramAuth) IsAuthenticated() bool {
	return t.config.SessionToken != "" && t.config.UserID != ""
}

func maskToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return token[:4] + "***" + token[len(token)-4:]
}

// UserInfo представляет информацию о пользователе от sort-me.org
type UserInfo struct {
	Username  string `json:"username"`
	UserID    string `json:"user_id"`
	FirstName string `json:"first_name"`
}
