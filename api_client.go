package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type APIClient struct {
	config  *Config
	client  *http.Client
	baseURL string
}

// Структуры для API sort-me.org
type SubmitRequest struct {
	TaskID    int    `json:"task_id"`
	Lang      string `json:"lang"`
	Code      string `json:"code"`
	ContestID int    `json:"contest_id"`
}

type SubmitResponse struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Error   string `json:"error"`
}

type SubmissionStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Result string `json:"result"`
	Score  int    `json:"score"`
	Time   string `json:"time"`
	Memory string `json:"memory"`
}

type WSMessage struct {
	Type   string      `json:"type"`
	Data   interface{} `json:"data"`
	Status string      `json:"status"`
	Result string      `json:"result"`
	Score  int         `json:"score"`
	Time   string      `json:"time"`
	Memory string      `json:"memory"`
}

type SubmissionResult struct {
	Compiled         bool      `json:"compiled"`
	CompilerLog      string    `json:"compiler_log"`
	ShownVerdict     int       `json:"shown_verdict"`
	ShownVerdictText string    `json:"shown_verdict_text"`
	TotalPoints      int       `json:"total_points"`
	Subtasks         []Subtask `json:"subtasks"`
}

type Subtask struct {
	Skipped     bool        `json:"skipped"`
	Points      int         `json:"points"`
	FailedTests interface{} `json:"failed_tests"`
	WorstTime   int         `json:"worst_time"`
}

// Структуры для списка отправок
type Submission struct {
	ID               int    `json:"id"`
	ShownTest        int    `json:"shown_test"`
	ShownVerdict     int    `json:"shown_verdict"`
	ShownVerdictText string `json:"shown_verdict_text"`
	TotalPoints      int    `json:"total_points"`
	ContestID        string `json:"contest_id,omitempty"`
	ProblemID        int    `json:"problem_id,omitempty"`
	Language         string `json:"language,omitempty"`
	Time             string `json:"time,omitempty"`
}

type SubmissionsResponse struct {
	Count       int          `json:"count"`
	Submissions []Submission `json:"submissions"`
}

// Структуры для контестов и задач
type ContestInfo struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Starts      int64  `json:"starts"`
	Ends        int64  `json:"ends"`
	Registered  bool   `json:"registered"`
	Tasks       []Task `json:"tasks"`
	Description string `json:"description,omitempty"`
}

type Task struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type Contest struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Started bool   `json:"started"`
}

type Problem struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
}

func NewAPIClient(config *Config) *APIClient {
	return &APIClient{
		config: config,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		// ПРАВИЛЬНЫЙ BASE URL - API сервер
		baseURL: "https://api.sort-me.org",
	}
}

func (a *APIClient) SubmitSolution(contestID, problemID, language, sourceCode string) (*SubmitResponse, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	// Конвертируем строки в числа
	contestIDInt, err := strconv.Atoi(contestID)
	if err != nil {
		return nil, fmt.Errorf("invalid contest ID: %s", contestID)
	}

	problemIDInt, err := strconv.Atoi(problemID)
	if err != nil {
		return nil, fmt.Errorf("invalid problem ID: %s", problemID)
	}

	// Правильная структура с числами
	requestData := SubmitRequest{
		TaskID:    problemIDInt,
		Lang:      language,
		Code:      sourceCode,
		ContestID: contestIDInt,
	}

	jsonData, err := json.Marshal(requestData)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	fmt.Printf("📡 Отправка запроса на %s/submit\n", a.baseURL)
	fmt.Printf("📦 Данные: contest_id=%d, task_id=%d, lang=%s\n", contestIDInt, problemIDInt, language)

	req, err := http.NewRequest("POST", a.baseURL+"/submit", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Headers из найденного запроса
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)

	fmt.Printf("🔑 Используется токен: %s\n", maskToken(a.config.SessionToken))

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("📥 Ответ сервера: Status %d\n", resp.StatusCode)

	// Успешные статусы: 200 OK, 201 Created
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API вернул ошибку %d: %s", resp.StatusCode, string(body))
	}

	var apiResponse SubmitResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		// Если не можем распарсить JSON, но статус успешный - создаем базовый ответ
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			return &SubmitResponse{
				ID:      string(body),
				Status:  "submitted",
				Message: "Решение успешно отправлено",
			}, nil
		}
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &apiResponse, nil
}

func (a *APIClient) GetSubmissionStatus(submissionID string) (*SubmissionStatus, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	// Сначала пробуем REST
	status, err := a.tryRESTStatus(submissionID)
	if err == nil {
		return status, nil
	}

	// Если REST не работает, используем WebSocket
	fmt.Printf("🔌 Подключаемся к WebSocket для статуса %s\n", submissionID)
	return a.getStatusViaWebSocket(submissionID)
}

func (a *APIClient) getStatusViaWebSocket(submissionID string) (*SubmissionStatus, error) {
	// Создаем WebSocket URL
	wsURL := "wss://api.sort-me.org/ws/submission?id=" + submissionID + "&token=" + a.config.SessionToken

	fmt.Printf("🔗 WebSocket URL: wss://api.sort-me.org/ws/submission?id=%s&token=%s\n",
		submissionID, maskToken(a.config.SessionToken))

	// Создаем соединение
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("WebSocket connection failed: %w", err)
	}
	defer conn.Close()

	fmt.Println("✅ WebSocket подключен успешно")
	fmt.Println("⏳ Ожидаем финальный статус...")

	// Устанавливаем общий таймаут 60 секунд
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	var lastStatus *SubmissionStatus

	// Читаем сообщения пока не получим финальный статус или не истечет время
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				if lastStatus != nil {
					fmt.Printf("⏰ Таймаут, возвращаем последний известный статус: %s\n", lastStatus.Status)
					return lastStatus, nil
				}
				return nil, fmt.Errorf("таймаут ожидания статуса")
			}
			return nil, fmt.Errorf("WebSocket read error: %w", err)
		}

		if messageType == websocket.TextMessage {
			fmt.Printf("📨 Получено сообщение (%d байт)\n", len(message))

			// Парсим полученное сообщение
			status, err := a.parseWebSocketMessage(message)
			if err != nil {
				fmt.Printf("❌ Ошибка парсинга: %v\n", err)
				continue
			}
			status.ID = submissionID
			lastStatus = status

			// Выводим текущий статус
			fmt.Printf("📊 Текущий статус: %s", getStatusEmoji(status.Status))
			if status.Score > 0 {
				fmt.Printf(" (%d баллов)", status.Score)
			}
			if status.Time != "" {
				fmt.Printf(" ⏱️ %s", status.Time)
			}
			fmt.Println()

			// Проверяем финальный ли это статус
			if a.isFinalStatus(status.Status) {
				fmt.Printf("🎯 Получен финальный статус: %s\n", getStatusEmoji(status.Status))
				return status, nil
			}

			// Обновляем таймаут для следующего чтения
			conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		}
	}
}

func (a *APIClient) tryContestEndpoint(endpoint string) ([]Contest, error) {
	req, err := http.NewRequest("GET", a.baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	return a.parseContestResponse(body, endpoint)
}

func (a *APIClient) parseContestResponse(body []byte, endpoint string) ([]Contest, error) {
	fmt.Printf("🔍 Парсим ответ от %s\n", endpoint)

	// 1. Для getUpcomingContests - это массив предстоящих контестов
	if strings.Contains(endpoint, "getUpcomingContests") {
		var upcomingContests []struct {
			ID                 int    `json:"id"`
			Name               string `json:"name"`
			Starts             int64  `json:"starts"`
			Ends               int64  `json:"ends"`
			OrgName            string `json:"org_name"`
			Running            bool   `json:"running"`
			RegistrationOpened bool   `json:"registration_opened"`
			Ended              bool   `json:"ended"`
		}

		if err := json.Unmarshal(body, &upcomingContests); err == nil && len(upcomingContests) > 0 {
			fmt.Printf("✅ Найдено %d предстоящих/активных контестов\n", len(upcomingContests))

			var contests []Contest
			for _, uc := range upcomingContests {
				status := "upcoming"
				if uc.Running {
					status = "active"
				} else if uc.Ended {
					status = "ended"
				}

				contests = append(contests, Contest{
					ID:      fmt.Sprintf("%d", uc.ID),
					Name:    uc.Name,
					Status:  status,
					Started: uc.Running,
				})
			}
			return contests, nil
		}
	}

	// 2. Для getArchiveCategories - это объект с категориями архива
	if strings.Contains(endpoint, "getArchiveCategories") {
		var categories map[string]int
		if err := json.Unmarshal(body, &categories); err == nil && len(categories) > 0 {
			fmt.Printf("✅ Найдено %d архивных категорий\n", len(categories))

			var contests []Contest
			for categoryName := range categories {
				contests = append(contests, Contest{
					ID:     fmt.Sprintf("archive_%s", categoryName),
					Name:   fmt.Sprintf("Архив: %s", categoryName),
					Status: "archive",
				})
			}
			return contests, nil
		}
	}

	// 3. Для getArchivePreviews - это объект с архивными олимпиадами
	if strings.Contains(endpoint, "getArchivePreviews") {
		var archiveResponse struct {
			Count int `json:"count"`
			Items []struct {
				ID            int    `json:"id"`
				Name          string `json:"name"`
				Category      string `json:"category"`
				SolvedPercent int    `json:"solved_percent"`
				Seasons       int    `json:"seasons"`
			} `json:"items"`
		}

		if err := json.Unmarshal(body, &archiveResponse); err == nil && len(archiveResponse.Items) > 0 {
			fmt.Printf("✅ Найдено %d архивных олимпиад\n", len(archiveResponse.Items))

			var contests []Contest
			for _, item := range archiveResponse.Items {
				contests = append(contests, Contest{
					ID:     fmt.Sprintf("archive_%d", item.ID),
					Name:   fmt.Sprintf("%s (%s)", item.Name, item.Category),
					Status: "archive",
				})
			}
			return contests, nil
		}
	}

	return nil, fmt.Errorf("неизвестный формат ответа")
}

func (a *APIClient) addKnownContests(contests []Contest) []Contest {
	knownContests := map[string]Contest{
		"456": {
			ID:      "456",
			Name:    "Лабораторная АиСД ИТМО №2 (25/26)",
			Status:  "active",
			Started: true,
		},
		"457": {
			ID:      "457",
			Name:    "Лабораторная АиСД ИТМО №3 (25/26)",
			Status:  "active",
			Started: true,
		},
	}

	// Проверяем какие известные контесты уже есть в списке
	existingIDs := make(map[string]bool)
	for _, contest := range contests {
		existingIDs[contest.ID] = true
	}

	// Добавляем отсутствующие известные контесты
	for id, contest := range knownContests {
		if !existingIDs[id] {
			// Проверяем доступность контеста
			if a.isContestAccessible(id) {
				contests = append(contests, contest)
				fmt.Printf("✅ Добавлен известный контест: %s\n", contest.Name)
			}
		}
	}

	return contests
}

func (a *APIClient) isContestAccessible(contestID string) bool {
	// Проверяем доступность через запрос задач контеста
	endpoint := fmt.Sprintf("/getContestTasks?id=%s", contestID)

	req, err := http.NewRequest("GET", a.baseURL+endpoint, nil)
	if err != nil {
		return false
	}

	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	return resp.StatusCode == http.StatusOK
}

func (a *APIClient) getFallbackContests() []Contest {
	return []Contest{
		{
			ID:      "456",
			Name:    "Лабораторная АиСД ИТМО №2 (25/26)",
			Status:  "active",
			Started: true,
		},
		{
			ID:      "457",
			Name:    "Лабораторная АиСД ИТМО №3 (25/26)",
			Status:  "active",
			Started: true,
		},
	}
}

func (a *APIClient) parseWebSocketMessage(message []byte) (*SubmissionStatus, error) {
	fmt.Printf("🔍 Парсим WebSocket сообщение...\n")

	// Пробуем распарсить как SubmissionResult
	var result SubmissionResult
	if err := json.Unmarshal(message, &result); err == nil {
		fmt.Printf("✅ Успешно распарсено как SubmissionResult\n")
		return a.convertResultToStatus(result), nil
	}

	// Пробуем распарсить как WSMessage
	var wsMessage WSMessage
	if err := json.Unmarshal(message, &wsMessage); err == nil {
		fmt.Printf("✅ Успешно распарсено как WSMessage\n")
		return a.parseStatusMessage(wsMessage), nil
	}

	return nil, fmt.Errorf("неизвестный формат сообщения")
}

func (a *APIClient) convertResultToStatus(result SubmissionResult) *SubmissionStatus {
	status := &SubmissionStatus{
		ID:     "current",
		Score:  result.TotalPoints,
		Result: result.ShownVerdictText,
	}

	// Определяем статус на основе данных
	if !result.Compiled {
		status.Status = "compilation_error"
	} else if result.TotalPoints == 100 {
		status.Status = "accepted"
	} else if result.TotalPoints > 0 {
		status.Status = "partial"
	} else {
		status.Status = "wrong_answer"
	}

	// Добавляем время если есть
	if len(result.Subtasks) > 0 {
		status.Time = fmt.Sprintf("%d ms", result.Subtasks[0].WorstTime)
	}

	return status
}

func (a *APIClient) parseStatusMessage(message WSMessage) *SubmissionStatus {
	status := &SubmissionStatus{
		ID:     "",
		Status: message.Status,
		Result: message.Result,
		Score:  message.Score,
		Time:   message.Time,
		Memory: message.Memory,
	}

	// Парсим данные если они есть
	if data, ok := message.Data.(map[string]interface{}); ok {
		fmt.Printf("🔍 Данные: %+v\n", data)

		if id, exists := data["id"]; exists {
			status.ID = fmt.Sprintf("%v", id)
		}
		if statusVal, exists := data["status"]; exists {
			status.Status = fmt.Sprintf("%v", statusVal)
		}
		if result, exists := data["result"]; exists {
			status.Result = fmt.Sprintf("%v", result)
		}
		if score, exists := data["score"]; exists {
			if s, ok := score.(float64); ok {
				status.Score = int(s)
			}
		}
		if timeVal, exists := data["time"]; exists {
			status.Time = fmt.Sprintf("%v", timeVal)
		}
		if memory, exists := data["memory"]; exists {
			status.Memory = fmt.Sprintf("%v", memory)
		}
	}

	// Если ID пустой, используем submission ID из параметров
	if status.ID == "" {
		status.ID = "unknown"
	}

	return status
}

func (a *APIClient) isFinalStatus(status string) bool {
	finalStatuses := []string{
		"accepted", "wrong_answer", "time_limit_exceeded",
		"memory_limit_exceeded", "compilation_error", "runtime_error",
		"AC", "WA", "TLE", "MLE", "CE", "RE",
	}

	for _, s := range finalStatuses {
		if status == s {
			return true
		}
	}
	return false
}

func (a *APIClient) tryRESTStatus(submissionID string) (*SubmissionStatus, error) {
	endpoints := []string{
		"/submission/" + submissionID,
		"/submissions/" + submissionID,
		"/api/submission/" + submissionID,
		"/api/submissions/" + submissionID,
	}

	var lastError error
	for _, endpoint := range endpoints {
		status, err := a.tryGetStatus(endpoint)
		if err == nil {
			return status, nil
		}
		lastError = err
	}
	return nil, lastError
}

func (a *APIClient) tryGetStatus(endpoint string) (*SubmissionStatus, error) {
	req, err := http.NewRequest("GET", a.baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var status SubmissionStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, err
	}

	return &status, nil
}

// Методы для списка отправок
func (a *APIClient) GetSubmissions(limit int) ([]Submission, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	// Используем найденный endpoint для конкретной задачи
	endpoint := "/getMySubmissionsByTask?id=2472&contestid=456"

	fmt.Printf("🔍 Используем endpoint: %s\n", endpoint)

	submissions, err := a.tryGetSubmissions(endpoint)
	if err != nil {
		return nil, err
	}

	// Ограничиваем количество результатов
	if limit > 0 && limit < len(submissions) {
		return submissions[:limit], nil
	}

	return submissions, nil
}

func (a *APIClient) tryGetSubmissions(endpoint string) ([]Submission, error) {
	req, err := http.NewRequest("GET", a.baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)

	var response SubmissionsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return response.Submissions, nil
}

func (a *APIClient) GetContestInfo(contestID string) (*ContestInfo, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	// Используем найденный endpoint
	endpoint := "/getContestTasks?id=" + contestID

	fmt.Printf("🔍 Используем endpoint: %s\n", endpoint)

	return a.tryGetContestInfo(endpoint)
}

func (a *APIClient) tryGetContestInfo(endpoint string) (*ContestInfo, error) {
	req, err := http.NewRequest("GET", a.baseURL+endpoint, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)

	var contestInfo ContestInfo
	if err := json.Unmarshal(body, &contestInfo); err != nil {
		return nil, err
	}

	fmt.Printf("✅ Получена информация о контесте: %s\n", contestInfo.Name)
	return &contestInfo, nil
}

func (a *APIClient) GetContests() ([]Contest, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	// Реальные endpoints из исследования
	endpoints := []struct {
		path string
		name string
	}{
		{"/getUpcomingContests", "предстоящие контесты"},
		{"/getArchiveCategories", "архивные категории"},
		{"/getArchivePreviews?hidesolved=1&category=0&offset=0", "архивные олимпиады"},
	}

	var allContests []Contest

	for _, endpoint := range endpoints {
		contests, err := a.tryContestEndpoint(endpoint.path)
		if err == nil && len(contests) > 0 {
			allContests = append(allContests, contests...)
		}
	}

	// Добавляем известные контесты если их нет в списке
	allContests = a.addKnownContests(allContests)

	if len(allContests) == 0 {
		return a.getFallbackContests(), nil
	}

	return allContests, nil
}

func (a *APIClient) IsAuthenticated() bool {
	return a.config.SessionToken != "" && a.config.UserID != ""
}

func (a *APIClient) DetectLanguage(filename string) string {
	ext := filepath.Ext(filename)
	switch ext {
	case ".py":
		return "python"
	case ".java":
		return "java"
	case ".cpp", ".cc", ".cxx":
		return "c++"
	case ".c":
		return "c"
	case ".go":
		return "go"
	case ".js":
		return "javascript"
	case ".rs":
		return "rust"
	default:
		return "unknown"
	}
}

func ReadSourceCode(filename string) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(content), nil
}

func maskToken(token string) string {
	if len(token) <= 8 {
		return "***"
	}
	return token[:4] + "***" + token[len(token)-4:]
}
