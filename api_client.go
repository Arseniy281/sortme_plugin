package main

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
	Status  string `json:"status"`  // active, upcoming, archive
	Started bool   `json:"started"` // Добавляем это поле
}

// В методе getArchiveContestSubmissions уберем лишний вывод
func (a *APIClient) getArchiveContestSubmissions(contestID string, contestInfo *ContestInfo, limit int) ([]Submission, error) {
	insecureClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	// Пробуем разные endpoints для архивных контестов (тихо, без вывода)
	endpoints := []string{
		fmt.Sprintf("/getArchiveSubmissions?contest_id=%s", contestID),
		fmt.Sprintf("/getMyArchiveSubmissions?contest_id=%s", contestID),
		fmt.Sprintf("/archive/%s/submissions", contestID),
	}

	for _, endpoint := range endpoints {
		url := "https://94.103.85.238" + endpoint

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}

		req.Host = "api.sort-me.org"
		req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
		req.Header.Set("Accept", "application/json")

		resp, err := insecureClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(resp.Body)

			// Пробуем разные форматы ответа
			foundSubmissions, err := a.parseArchiveSubmissions(body, contestInfo)
			if err == nil && len(foundSubmissions) > 0 {
				return foundSubmissions, nil
			}
		}
	}

	// Если специальные endpoints не работают, пробуем получить отправки через общий метод
	return a.getSubmissionsViaTasks(contestID, contestInfo, limit)
}

// В методе getSubmissionsViaTasks упростим вывод
func (a *APIClient) getSubmissionsViaTasks(contestID string, contestInfo *ContestInfo, limit int) ([]Submission, error) {
	var allSubmissions []Submission

	for i, task := range contestInfo.Tasks {
		// Добавляем небольшую задержку между запросами
		if i > 0 {
			time.Sleep(100 * time.Millisecond)
		}

		endpoint := fmt.Sprintf("/getMySubmissionsByTask?id=%d", task.ID)
		taskSubmissions, err := a.tryGetSubmissions(endpoint, 0)
		if err != nil {
			continue
		}

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

	// Применяем лимит
	if limit > 0 && limit < len(allSubmissions) {
		return allSubmissions[:limit], nil
	}

	return allSubmissions, nil
}

// В методе tryGetSubmissions убедитесь что он получает все отправки
func (a *APIClient) tryGetSubmissions(endpoint string, limit int) ([]Submission, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	baseURL := "https://94.103.85.238"
	fullURL := baseURL + endpoint

	req, err := http.NewRequest("GET", fullURL, nil)
	if err != nil {
		return nil, err
	}

	req.Host = "api.sort-me.org"
	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == 404 {
			return []Submission{}, nil
		}
		if resp.StatusCode == 429 {
			time.Sleep(1 * time.Second)
			return []Submission{}, fmt.Errorf("rate limit")
		}
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)

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

	// Если limit не указан, возвращаем все отправки
	if limit <= 0 {
		return response.Submissions, nil
	}

	if limit < len(response.Submissions) {
		return response.Submissions[:limit], nil
	}

	return response.Submissions, nil
}

// В методе GetContestSubmissions упростим вывод
func (a *APIClient) GetContestSubmissions(contestID string, limit int) ([]Submission, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	// Получаем информацию о контесте
	contestInfo, err := a.GetContestInfo(contestID)
	if err != nil {
		return nil, fmt.Errorf("не удалось получить информацию о контесте: %w", err)
	}

	// Для архивных контестов используем специальный метод
	if contestInfo.Status == "archive" {
		return a.getArchiveContestSubmissions(contestID, contestInfo, limit)
	}

	var allSubmissions []Submission

	// Для обычных контестов используем старый метод
	for _, task := range contestInfo.Tasks {
		taskSubmissions, err := a.tryGetSubmissions(fmt.Sprintf("/getMySubmissionsByTask?id=%d&contestid=%s", task.ID, contestID), 0)
		if err != nil {
			continue
		}

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

	// Применяем лимит
	if limit > 0 && limit < len(allSubmissions) {
		return allSubmissions[:limit], nil
	}

	return allSubmissions, nil
}

// В методе parseArchiveSubmissions убираем неиспользуемую переменную
func (a *APIClient) parseArchiveSubmissions(body []byte, contestInfo *ContestInfo) ([]Submission, error) {
	// Убрали объявление resultSubmissions так как она не используется

	// Формат 1: Прямой массив отправок
	var directSubmissions []Submission
	if err := json.Unmarshal(body, &directSubmissions); err == nil && len(directSubmissions) > 0 {
		fmt.Printf("     📝 Формат: прямой массив отправок\n")
		// Обогащаем данные информацией о контесте
		for i := range directSubmissions {
			directSubmissions[i].ContestID = fmt.Sprintf("%d", contestInfo.ID) // Конвертируем int в string
			directSubmissions[i].ContestName = contestInfo.Name
			// Находим имя задачи по ID
			for _, task := range contestInfo.Tasks {
				if task.ID == directSubmissions[i].ProblemID {
					directSubmissions[i].ProblemName = task.Name
					break
				}
			}
		}
		return directSubmissions, nil
	}

	// Формат 2: Объект с полем submissions
	var withSubmissionsField struct {
		Submissions []Submission `json:"submissions"`
		Count       int          `json:"count"`
	}
	if err := json.Unmarshal(body, &withSubmissionsField); err == nil && withSubmissionsField.Submissions != nil {
		fmt.Printf("     📝 Формат: объект с submissions\n")
		for i := range withSubmissionsField.Submissions {
			withSubmissionsField.Submissions[i].ContestID = fmt.Sprintf("%d", contestInfo.ID) // Конвертируем int в string
			withSubmissionsField.Submissions[i].ContestName = contestInfo.Name
			for _, task := range contestInfo.Tasks {
				if task.ID == withSubmissionsField.Submissions[i].ProblemID {
					withSubmissionsField.Submissions[i].ProblemName = task.Name
					break
				}
			}
		}
		return withSubmissionsField.Submissions, nil
	}

	return nil, fmt.Errorf("неизвестный формат ответа")
}

// ФИНАЛЬНАЯ РЕАЛИЗАЦИЯ GetContests
func (a *APIClient) GetContests() ([]Contest, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	fmt.Println("🏆 Получение контестов...")

	var allContests []Contest

	// 1. АКТИВНЫЕ И ПРЕДСТОЯЩИЕ КОНТЕСТЫ
	activeContests, err := a.getUpcomingContests()
	if err != nil {
		fmt.Printf("⚠️ Не удалось получить активные контесты: %v\n", err)
	} else {
		allContests = append(allContests, activeContests...)
		fmt.Printf("🎯 Активные/предстоящие контесты: %d\n", len(activeContests))
	}

	// 2. АРХИВНЫЕ КОНТЕСТЫ
	archiveContests, err := a.getArchiveContestsViaIP()
	if err != nil {
		fmt.Printf("⚠️ Не удалось получить архивные контесты: %v\n", err)
	} else {
		allContests = append(allContests, archiveContests...)
		fmt.Printf("📚 Архивные контесты: %d\n", len(archiveContests))
	}

	if len(allContests) == 0 {
		return nil, fmt.Errorf("контесты не найдены")
	}

	// Обработка результатов
	allContests = a.removeDuplicateContests(allContests)
	allContests = a.sortContestsByStatus(allContests)

	// Статистика
	activeCount, archiveCount, upcomingCount := a.countContestsByDetailedStatus(allContests)

	fmt.Printf("✅ Итого: %d контестов\n", len(allContests))
	fmt.Printf("📊 Активных: %d, Предстоящих: %d, Архивных: %d\n",
		activeCount, upcomingCount, archiveCount)

	return allContests, nil
}

// Метод для получения активных/предстоящих контестов
func (a *APIClient) getUpcomingContests() ([]Contest, error) {
	insecureClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	url := "https://94.103.85.238/getUpcomingContests"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Host = "api.sort-me.org"
	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := insecureClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)

	var upcomingContests []UpcomingContest
	if err := json.Unmarshal(body, &upcomingContests); err != nil {
		return nil, err
	}

	return a.convertUpcomingToContests(upcomingContests), nil
}

// Структура для предстоящих контестов
type UpcomingContest struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Starts int64  `json:"starts"`
	Ends   int64  `json:"ends"`
}

// Конвертация в общую структуру Contest
// Конвертация в общую структуру Contest
func (a *APIClient) convertUpcomingToContests(upcoming []UpcomingContest) []Contest {
	var contests []Contest
	currentTime := time.Now().Unix()

	for _, uc := range upcoming {
		status := "active"
		started := true // по умолчанию считаем что начался

		if uc.Starts > currentTime {
			status = "upcoming"
			started = false // еще не начался
		} else if uc.Ends < currentTime {
			status = "archive"
		}

		contests = append(contests, Contest{
			ID:      fmt.Sprintf("%d", uc.ID),
			Name:    uc.Name,
			Status:  status,
			Started: started,
		})

		timeStatus := "активный"
		if status == "upcoming" {
			timeStatus = "предстоящий"
		} else if status == "archive" {
			timeStatus = "архивный"
		}

		fmt.Printf("   🎯 %s: %s (%s)\n", uc.Name, fmt.Sprintf("%d", uc.ID), timeStatus)
	}

	return contests
}

// ВСПОМОГАТЕЛЬНЫЕ МЕТОДЫ

// Удаление дубликатов контестов
func (a *APIClient) removeDuplicateContests(contests []Contest) []Contest {
	seen := make(map[string]bool)
	var result []Contest

	for _, contest := range contests {
		if !seen[contest.ID] {
			seen[contest.ID] = true
			result = append(result, contest)
		}
	}

	return result
}

// Сортировка контестов по статусу (активные -> предстоящие -> архивные)
func (a *APIClient) sortContestsByStatus(contests []Contest) []Contest {
	var active, upcoming, archive []Contest

	for _, contest := range contests {
		switch contest.Status {
		case "active":
			active = append(active, contest)
		case "upcoming":
			upcoming = append(upcoming, contest)
		case "archive":
			archive = append(archive, contest)
		}
	}

	// Собираем в правильном порядке
	var result []Contest
	result = append(result, active...)
	result = append(result, upcoming...)
	result = append(result, archive...)

	return result
}

// Подсчет контестов по статусам
func (a *APIClient) countContestsByDetailedStatus(contests []Contest) (active, archive, upcoming int) {
	for _, contest := range contests {
		switch contest.Status {
		case "active":
			active++
		case "archive":
			archive++
		case "upcoming":
			upcoming++
		}
	}
	return
}

// Метод для получения архивных контестов (должен уже быть)
// Метод для получения архивных контестов
func (a *APIClient) getArchiveContestsViaIP() ([]Contest, error) {
	insecureClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	url := "https://94.103.85.238/getArchivePreviews"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Host = "api.sort-me.org"
	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := insecureClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)

	var response struct {
		Count int `json:"count"`
		Items []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"items"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return nil, err
	}

	var contests []Contest
	for _, item := range response.Items {
		contests = append(contests, Contest{
			ID:      fmt.Sprintf("%d", item.ID),
			Name:    item.Name,
			Status:  "archive",
			Started: true, // архивные контесты уже начались
		})
	}

	return contests, nil
}

func (a *APIClient) GetContestInfo(contestID string) (*ContestInfo, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	fmt.Printf("📚 Получение информации о контесте %s...\n", contestID)

	// Конвертируем ID в число
	contestIDInt, err := strconv.Atoi(contestID)
	if err != nil {
		return nil, fmt.Errorf("неверный ID контеста: %s", contestID)
	}

	// Пробуем разные методы для получения информации о контесте
	return a.getContestInfoUniversal(contestIDInt)
}

func (a *APIClient) getContestInfoUniversal(contestID int) (*ContestInfo, error) {
	// Метод 1: Стандартный endpoint для обычных контестов
	if contestInfo, err := a.tryStandardEndpoint(contestID); err == nil {
		return contestInfo, nil
	}

	// Метод 2: Archive endpoint для архивных контестов
	if contestInfo, err := a.tryArchiveEndpoint(contestID); err == nil {
		return contestInfo, nil
	}

	return nil, fmt.Errorf("контест %d недоступен", contestID)
}

func (a *APIClient) tryStandardEndpoint(contestID int) (*ContestInfo, error) {
	insecureClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	endpoint := fmt.Sprintf("/getContestTasks?id=%d", contestID)
	url := "https://94.103.85.238" + endpoint

	fmt.Printf("  📡 Стандартный endpoint: %s\n", endpoint)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Host = "api.sort-me.org"
	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := insecureClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var contestInfo ContestInfo
	if err := json.Unmarshal(body, &contestInfo); err != nil {
		return nil, err
	}

	fmt.Printf("  ✅ Контест: %s, задач: %d\n", contestInfo.Name, len(contestInfo.Tasks))
	return &contestInfo, nil
}

func (a *APIClient) tryArchiveEndpoint(contestID int) (*ContestInfo, error) {
	insecureClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	endpoint := fmt.Sprintf("/getArchiveById?id=%d", contestID)
	url := "https://94.103.85.238" + endpoint

	fmt.Printf("  📡 Archive endpoint: %s\n", endpoint)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Host = "api.sort-me.org"
	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Header.Set("Accept", "application/json")

	resp, err := insecureClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Парсим архивные данные
	var archiveData struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Seasons []struct {
			Name          string `json:"name"`
			SourceContest int    `json:"source_contest"`
			Tasks         []Task `json:"tasks"`
		} `json:"seasons"`
	}

	if err := json.Unmarshal(body, &archiveData); err != nil {
		return nil, fmt.Errorf("ошибка парсинга: %w", err)
	}

	// Собираем все задачи из всех seasons
	var allTasks []Task
	for _, season := range archiveData.Seasons {
		allTasks = append(allTasks, season.Tasks...)
	}

	fmt.Printf("  ✅ Архивный контест: %s, seasons: %d, задач: %d\n",
		archiveData.Name, len(archiveData.Seasons), len(allTasks))

	return &ContestInfo{
		ID:     archiveData.ID,
		Name:   archiveData.Name,
		Status: "archive",
		Tasks:  allTasks,
	}, nil
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

func cleanSubmissionID(submissionID string) string {
	// Если ID приходит в формате JSON, извлекаем числовое значение
	if strings.HasPrefix(submissionID, "{") && strings.Contains(submissionID, "id") {
		var response struct {
			ID interface{} `json:"id"`
		}
		if err := json.Unmarshal([]byte(submissionID), &response); err == nil {
			if response.ID != nil {
				return fmt.Sprintf("%v", response.ID)
			}
		}
	}
	return submissionID
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

	fmt.Printf("📡 Отправка решения...\n")
	fmt.Printf("📦 Данные: contest_id=%d, task_id=%d, lang=%s\n", contestIDInt, problemIDInt, language)

	// Используем прямое IP подключение для отправки
	return a.submitViaIP(jsonData)
}

func (a *APIClient) submitViaIP(jsonData []byte) (*SubmitResponse, error) {
	insecureClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	url := "https://94.103.85.238/submit"
	fmt.Printf("🌐 Отправка через IP: %s\n", url)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.SessionToken)
	req.Host = "api.sort-me.org"

	fmt.Printf("🔑 Используется токен: %s\n", maskToken(a.config.SessionToken))

	resp, err := insecureClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	fmt.Printf("📥 Ответ сервера: Status %d\n", resp.StatusCode)
	fmt.Printf("📦 Тело ответа: %s\n", string(body)) // Добавьте это для отладки

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API вернул ошибку %d: %s", resp.StatusCode, string(body))
	}

	var apiResponse SubmitResponse
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		// Если не можем распарсить JSON, но статус успешный - пробуем извлечь ID из ответа
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
			// Пробуем распарсить как объект с полем id
			var responseObj map[string]interface{}
			if err := json.Unmarshal(body, &responseObj); err == nil {
				if id, exists := responseObj["id"]; exists {
					// Конвертируем ID в строку независимо от его типа
					apiResponse.ID = fmt.Sprintf("%v", id)
					apiResponse.Status = "submitted"
					apiResponse.Message = "Решение успешно отправлено"
					return &apiResponse, nil
				}
			}

			// Если не удалось распарсить как объект, возвращаем как есть
			return &SubmitResponse{
				ID:      string(body),
				Status:  "submitted",
				Message: "Решение успешно отправлено",
			}, nil
		}
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Убедимся, что ID в правильном формате
	if apiResponse.ID == "" {
		// Если ID пустой в JSON ответе, но есть в другом поле
		var responseObj map[string]interface{}
		if err := json.Unmarshal(body, &responseObj); err == nil {
			if id, exists := responseObj["id"]; exists {
				apiResponse.ID = fmt.Sprintf("%v", id)
			}
		}
	}

	return &apiResponse, nil
}

func (a *APIClient) getStatusViaWebSocket(submissionID string) (*SubmissionStatus, error) {
	// Создаем WebSocket URL с IP
	wsURL := "wss://94.103.85.238/ws/submission?id=" + submissionID + "&token=" + a.config.SessionToken

	fmt.Printf("🔗 WebSocket URL: wss://api.sort-me.org/ws/submission?id=%s&token=%s\n",
		submissionID, maskToken(a.config.SessionToken))

	// Создаем соединение
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
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
			if status.Memory != "" {
				fmt.Printf(" 💾 %s", status.Memory)
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

// Методы для списка отправок
func (a *APIClient) GetSubmissions(limit int) ([]Submission, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	// Получаем отправки только из активных контестов
	return a.getAllSubmissions(limit)
}

// Быстрый метод для получения последних отправок
func (a *APIClient) GetRecentSubmissions(limit int) ([]Submission, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	fmt.Printf("🔍 Поиск %d последних отправок...\n", limit)

	// Пробуем получить отправки только из доступных контестов
	contests, err := a.GetContests()
	if err != nil {
		return nil, err
	}

	// Берем только первые 2 контеста для скорости
	if len(contests) > 2 {
		contests = contests[:2]
	}

	var allSubmissions []Submission

	for _, contest := range contests {
		fmt.Printf("📚 Контест: %s... ", contest.Name)

		// Получаем только первые 3 задачи контеста
		contestInfo, err := a.GetContestInfo(contest.ID)
		if err != nil {
			fmt.Printf("❌\n")
			continue
		}

		if len(contestInfo.Tasks) == 0 {
			fmt.Printf("📭\n")
			continue
		}

		// Ограничиваем количество задач
		maxTasks := 3
		if len(contestInfo.Tasks) > maxTasks {
			contestInfo.Tasks = contestInfo.Tasks[:maxTasks]
		}

		var contestSubmissions []Submission

		for _, task := range contestInfo.Tasks {
			// Получаем только последние 2 отправки для каждой задачи
			submissions, err := a.tryGetSubmissions(fmt.Sprintf("/getMySubmissionsByTask?id=%d&contestid=%s", task.ID, contest.ID), 2)
			if err != nil {
				continue
			}

			// Добавляем информацию о задаче
			for i := range submissions {
				submissions[i].ProblemID = task.ID
				submissions[i].ProblemName = task.Name
				submissions[i].ContestID = contest.ID
				submissions[i].ContestName = contestInfo.Name
			}

			contestSubmissions = append(contestSubmissions, submissions...)
		}

		fmt.Printf("✅ %d отправок\n", len(contestSubmissions))
		allSubmissions = append(allSubmissions, contestSubmissions...)
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

// Получить все отправки (оптимизированная версия)
func (a *APIClient) getAllSubmissions(limit int) ([]Submission, error) {
	// Получаем реальные контесты через API
	contests, err := a.GetContests()
	if err != nil {
		return nil, fmt.Errorf("не удалось получить список контестов: %w", err)
	}

	var allSubmissions []Submission

	fmt.Printf("🔍 Поиск отправок в %d контестах...\n", len(contests))

	// Ограничиваем количество проверяемых контестов для скорости
	maxContests := 3
	if len(contests) > maxContests {
		fmt.Printf("⚠️  Ограничиваем до %d контестов для скорости\n", maxContests)
		contests = contests[:maxContests]
	}

	for i, contest := range contests {
		fmt.Printf("📚 Контест %d/%d: %s\n", i+1, len(contests), contest.Name)

		// Получаем информацию о контесте
		contestInfo, err := a.GetContestInfo(contest.ID)
		if err != nil {
			fmt.Printf("   ⚠️  Не удалось получить задачи: %v\n", err)
			continue
		}

		fmt.Printf("📚 Задачи контеста (%d): ", len(contestInfo.Tasks))

		var contestSubmissions []Submission

		// Ограничиваем количество проверяемых задач для скорости
		maxTasks := 5
		tasksToCheck := contestInfo.Tasks
		if len(tasksToCheck) > maxTasks {
			tasksToCheck = tasksToCheck[:maxTasks]
		}

		// Последовательно получаем отправки для каждой задачи
		for j, task := range tasksToCheck {
			// Увеличиваем задержку чтобы избежать rate limiting
			if j > 0 {
				time.Sleep(500 * time.Millisecond) // Увеличили до 500мс
			}

			taskSubmissions, err := a.tryGetSubmissions(fmt.Sprintf("/getMySubmissionsByTask?id=%d&contestid=%s", task.ID, contest.ID), 5) // Ограничиваем 5 отправок на задачу
			if err != nil {
				fmt.Printf("❌") // Просто крестик без текста
				continue
			}

			fmt.Printf("✅") // Галочка для успешной загрузки

			// Добавляем информацию о задаче к каждой отправке
			for k := range taskSubmissions {
				taskSubmissions[k].ProblemID = task.ID
				taskSubmissions[k].ProblemName = task.Name
				taskSubmissions[k].ContestID = contest.ID
				taskSubmissions[k].ContestName = contestInfo.Name
			}

			contestSubmissions = append(contestSubmissions, taskSubmissions...)
		}

		allSubmissions = append(allSubmissions, contestSubmissions...)
		fmt.Printf(" | %d отправок\n", len(contestSubmissions))
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
