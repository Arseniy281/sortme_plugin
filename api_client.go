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
	"sort"
	"strconv"
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
	ProblemName      string `json:"problem_name,omitempty"`
	ContestName      string `json:"contest_name,omitempty"`
	SubmitTime       string `json:"submit_time,omitempty"`
	TaskID           int    `json:"task_id,omitempty"`
	TaskName         string `json:"task_name,omitempty"`
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
			contests = append(contests, contest)
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
// Методы для списка отправок
func (a *APIClient) GetSubmissions(limit int) ([]Submission, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	// Получаем отправки только из активных контестов
	return a.getAllSubmissions(limit)
}

// Получить отправки для задачи во всех контестах
func (a *APIClient) getSubmissionsByTaskAcrossContests(taskID string, limit int) ([]Submission, error) {
	fmt.Printf("🌐 Получение списка контестов для поиска задачи %s...\n", taskID)

	contests, err := a.GetContests()
	if err != nil {
		return nil, fmt.Errorf("не удалось получить список контестов: %w", err)
	}

	var allSubmissions []Submission

	// Ограничиваем количество проверяемых контестов для производительности
	maxContests := 10
	if len(contests) > maxContests {
		fmt.Printf("📊 Ограничиваем проверку до %d последних контестов\n", maxContests)
		contests = contests[:maxContests]
	}

	for i, contest := range contests {
		fmt.Printf("🔍 Проверяем контест %d/%d (ID: %s)...\n", i+1, len(contests), contest.ID)

		// Проверяем существует ли задача в этом контесте
		contestInfo, err := a.GetContestInfo(contest.ID)
		if err != nil {
			continue
		}

		// Ищем задачу с нужным ID
		taskExists := false
		for _, task := range contestInfo.Tasks {
			if fmt.Sprintf("%d", task.ID) == taskID {
				taskExists = true
				break
			}
		}

		if taskExists {
			fmt.Printf("✅ Задача %s найдена в контесте %s\n", taskID, contest.ID)
			taskSubmissions, err := a.tryGetSubmissions(fmt.Sprintf("/getMySubmissionsByTask?id=%s&contestid=%s", taskID, contest.ID), 0)
			if err != nil {
				fmt.Printf("⚠️  Ошибка получения отправок: %v\n", err)
				continue
			}

			// Добавляем информацию о контесте к каждой отправке
			for j := range taskSubmissions {
				taskSubmissions[j].ProblemID, _ = strconv.Atoi(taskID)
				taskSubmissions[j].ContestID = contest.ID
				taskSubmissions[j].ContestName = contest.Name
			}

			allSubmissions = append(allSubmissions, taskSubmissions...)

			// Если нашли достаточно отправок, можно остановиться
			if limit > 0 && len(allSubmissions) >= limit {
				break
			}
		}
	}

	// Сортируем по ID (более новые сначала)
	sort.Slice(allSubmissions, func(i, j int) bool {
		return allSubmissions[i].ID > allSubmissions[j].ID
	})

	// Применяем лимит
	if limit > 0 && limit < len(allSubmissions) {
		return allSubmissions[:limit], nil
	}

	return allSubmissions, nil
}

// Получить отправки для конкретного контеста
// Получить отправки для конкретного контеста
func (a *APIClient) getSubmissionsByContest(contestID string, limit int) ([]Submission, error) {
	contestInfo, err := a.GetContestInfo(contestID)
	if err != nil {
		return nil, err
	}

	var allSubmissions []Submission

	fmt.Printf("📚 Задачи контеста (%d): ", len(contestInfo.Tasks))

	// Последовательно получаем отправки для каждой задачи
	for i, task := range contestInfo.Tasks {
		// Увеличиваем задержку чтобы избежать rate limiting
		if i > 0 {
			time.Sleep(200 * time.Millisecond) // Увеличили до 200мс
		}

		taskSubmissions, err := a.tryGetSubmissions(fmt.Sprintf("/getMySubmissionsByTask?id=%d&contestid=%s", task.ID, contestID), 0)
		if err != nil {
			fmt.Printf("❌") // Просто крестик без текста
			continue
		}

		fmt.Printf("✅") // Галочка для успешной загрузки

		// Добавляем информацию о задаче к каждой отправке
		for j := range taskSubmissions {
			taskSubmissions[j].ProblemID = task.ID
			taskSubmissions[j].ProblemName = task.Name
			taskSubmissions[j].ContestID = contestID
			taskSubmissions[j].ContestName = contestInfo.Name
		}

		allSubmissions = append(allSubmissions, taskSubmissions...)
	}

	// Сортируем по ID (более новые сначала)
	sort.Slice(allSubmissions, func(i, j int) bool {
		return allSubmissions[i].ID > allSubmissions[j].ID
	})

	fmt.Printf(" | %d отправок\n", len(allSubmissions))

	// Применяем лимит
	if limit > 0 && limit < len(allSubmissions) {
		return allSubmissions[:limit], nil
	}

	return allSubmissions, nil
}

// Получить все отправки (через известные контесты)
func (a *APIClient) getAllSubmissions(limit int) ([]Submission, error) {
	// Получаем только активные контесты
	contests, err := a.GetContests()
	if err != nil {
		return nil, fmt.Errorf("не удалось получить список контестов: %w", err)
	}

	var allSubmissions []Submission

	for i, contest := range contests {
		fmt.Printf("🔍 Контест %d/%d: %s\n", i+1, len(contests), contest.Name)

		contestSubmissions, err := a.getSubmissionsByContest(contest.ID, 0)
		if err != nil {
			continue
		}

		allSubmissions = append(allSubmissions, contestSubmissions...)
	}

	// Сортируем по ID (более новые сначала)
	sort.Slice(allSubmissions, func(i, j int) bool {
		return allSubmissions[i].ID > allSubmissions[j].ID
	})

	fmt.Printf("\n🎯 Итого: %d отправок\n", len(allSubmissions))

	// Применяем лимит
	if limit > 0 && limit < len(allSubmissions) {
		return allSubmissions[:limit], nil
	}

	return allSubmissions, nil
}

func (a *APIClient) tryGetSubmissions(endpoint string, limit int) ([]Submission, error) {
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
		// Для 404 возвращаем пустой список
		if resp.StatusCode == 404 {
			return []Submission{}, nil
		}
		// Для 429 (Too Many Requests) просто возвращаем пустой список
		if resp.StatusCode == 429 {
			return []Submission{}, fmt.Errorf("rate limit")
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)

	// Парсим ответ в правильном формате
	var response struct {
		Count       int          `json:"count"`
		Submissions []Submission `json:"submissions"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	// Сортируем по ID (более новые сначала)
	sort.Slice(response.Submissions, func(i, j int) bool {
		return response.Submissions[i].ID > response.Submissions[j].ID
	})

	// Ограничиваем количество результатов
	if limit > 0 && limit < len(response.Submissions) {
		return response.Submissions[:limit], nil
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

	// Используем только endpoint для предстоящих контестов
	endpoint := "/getUpcomingContests"

	req, err := http.NewRequest("GET", a.baseURL+endpoint, nil)
	if err != nil {
		return a.getFallbackContests(), nil
	}

	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return a.getFallbackContests(), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return a.getFallbackContests(), nil
	}

	body, _ := io.ReadAll(resp.Body)

	// Парсим ответ
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

	var contests []Contest
	if err := json.Unmarshal(body, &upcomingContests); err == nil {
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
	}

	// Добавляем известные контесты в любом случае
	contests = a.addKnownContests(contests)

	// Фильтруем только активные контесты
	var activeContests []Contest
	for _, contest := range contests {
		if contest.Status == "active" && contest.Started {
			activeContests = append(activeContests, contest)
		}
	}

	if len(activeContests) == 0 {
		return a.getFallbackContests(), nil
	}

	return activeContests, nil
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
