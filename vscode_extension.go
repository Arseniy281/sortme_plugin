package main

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type VSCodeExtension struct {
	config    *Config
	apiClient *APIClient
}

func NewVSCodeExtension() *VSCodeExtension {
	config, err := LoadConfig()
	if err != nil {
		fmt.Printf("Warning: failed to load config: %v\n", err)
		config = &Config{}
	}

	return &VSCodeExtension{
		config:    config,
		apiClient: NewAPIClient(config),
	}
}

func (v *VSCodeExtension) CreateRootCommand() *cobra.Command {
	var rootCmd = &cobra.Command{
		Use:   "sortme",
		Short: "Sort-me.org VSCode Plugin",
		Long:  "Плагин для отправки решений на sort-me.org через VSCode",
	}

	rootCmd.AddCommand(
		v.createAuthCommand(),
		v.createSubmitCommand(),
		v.createStatusCommand(),
		v.createWhoamiCommand(),
		v.createLogoutCommand(),
		v.createListCommand(),
		v.createProblemsCommand(),
		v.createDownloadCommand(),
		v.createContestsCommand(),
	)

	return rootCmd
}

func (v *VSCodeExtension) createContestsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "contests",
		Short: "Показать список доступных контестов",
		Run: func(cmd *cobra.Command, args []string) {
			v.handleContests()
		},
	}
}

func (v *VSCodeExtension) handleContests() {
	if !v.apiClient.IsAuthenticated() {
		fmt.Println("❌ Вы не аутентифицированы")
		return
	}

	fmt.Println("🏆 Поиск контестов...")

	contests, err := v.apiClient.GetContests()
	if err != nil {
		fmt.Printf("❌ Ошибка: %v\n", err)
		return
	}

	if len(contests) == 0 {
		fmt.Println("📭 Контесты не найдены")
		return
	}

	// Группируем контесты по статусу
	var active, archive []Contest
	for _, contest := range contests {
		if contest.Status == "active" && contest.Started {
			active = append(active, contest)
		} else if contest.Status == "archive" {
			archive = append(archive, contest)
		}
	}

	// Сначала показываем архивные
	if len(archive) > 0 {
		fmt.Printf("\n📚 Архивные контесты (%d):\n", len(archive))

		for i, contest := range archive {
			// Ограничиваем вывод до 8 контестов
			if i >= 8 {
				fmt.Printf("   ... и еще %d архивных контестов\n", len(archive)-8)
				break
			}

			name := contest.Name
			if len(name) > 40 {
				name = name[:37] + "..."
			}
			// ДОБАВЛЯЕМ ВЫВОД ID
			fmt.Printf("   🔴 %s (ID: %s)\n", name, contest.ID)
		}
	}

	// Затем активные контесты
	if len(active) > 0 {
		fmt.Printf("\n🎯 Актуальные контесты (%d):\n", len(active))
		for _, contest := range active {
			// ДОБАВЛЯЕМ ВЫВОД ID
			fmt.Printf("   🟢 %s (ID: %s)\n", contest.Name, contest.ID)
		}
	} else {
		fmt.Println("\n🎯 Актуальные контесты: нет активных контестов")
	}

	fmt.Printf("\n💡 Команды:\n")
	fmt.Printf("   sortme problems ID_контеста    - показать задачи контеста\n")
	fmt.Printf("   sortme submit файл -c ID -p ID - отправить решение\n")

	// Показываем пример с реальным ID из списка
	if len(archive) > 0 {
		fmt.Printf("   sortme problems %s         - пример\n", archive[0].ID)
	}

	// В конец handleContests добавим:
	fmt.Printf("\n🔢 Все ID контестов: ")
	for i, contest := range archive {
		if i > 0 {
			fmt.Printf(", ")
		}
		fmt.Printf("%s", contest.ID)
		if i >= 10 { // Ограничиваем вывод
			fmt.Printf("...")
			break
		}
	}
	fmt.Println()
}

func (v *VSCodeExtension) createAuthCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "auth",
		Short: "Аутентификация в sort-me.org",
		Long:  "Ввод данных аутентификации для работы с sort-me.org",
		Run: func(cmd *cobra.Command, args []string) {
			reader := bufio.NewReader(os.Stdin)

			fmt.Print("Введите ваш username: ")
			username, _ := reader.ReadString('\n')
			username = strings.TrimSpace(username)

			fmt.Print("Введите session token: ")
			token, _ := reader.ReadString('\n')
			token = strings.TrimSpace(token)

			v.config.Username = username
			v.config.SessionToken = token
			v.config.UserID = username

			if err := SaveConfig(v.config); err != nil {
				fmt.Printf("Ошибка сохранения: %v\n", err)
				return
			}

			fmt.Println("✅ Данные сохранены!")
			fmt.Printf("Username: %s\n", username)
			fmt.Printf("Token: %s\n", maskToken(token))
		},
	}
}

func (v *VSCodeExtension) createSubmitCommand() *cobra.Command {
	var contestID, problemID, language string

	cmd := &cobra.Command{
		Use:   "submit [file]",
		Short: "Отправить решение на проверку",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			filename := args[0]
			v.handleSubmit(filename, contestID, problemID, language)
		},
	}

	cmd.Flags().StringVarP(&contestID, "contest", "c", "", "ID контеста (обязательно)")
	cmd.Flags().StringVarP(&problemID, "problem", "p", "", "ID задачи (обязательно)")
	cmd.Flags().StringVarP(&language, "language", "l", "", "Язык программирования (опционально)")

	cmd.MarkFlagRequired("contest")
	cmd.MarkFlagRequired("problem")

	return cmd
}

func (v *VSCodeExtension) createStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status [submission_id]",
		Short: "Проверить статус отправки",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			submissionID := args[0]
			v.handleStatus(submissionID)
		},
	}
}

func (v *VSCodeExtension) createWhoamiCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Показать текущего пользователя",
		Run: func(cmd *cobra.Command, args []string) {
			if !v.apiClient.IsAuthenticated() {
				fmt.Println("❌ Вы не аутентифицированы")
				fmt.Println("Используйте команду:")
				fmt.Println("  sortme auth - для аутентификации")
				return
			}
			fmt.Printf("✅ Текущий пользователь: %s\n", v.config.Username)
			fmt.Printf("User ID: %s\n", v.config.UserID)
			fmt.Printf("Session token: %s\n", maskToken(v.config.SessionToken))
		},
	}
}

func (v *VSCodeExtension) createLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Выйти из системы",
		Run: func(cmd *cobra.Command, args []string) {
			v.config.SessionToken = ""
			v.config.UserID = ""
			v.config.Username = ""
			v.config.TelegramToken = ""

			if err := SaveConfig(v.config); err != nil {
				fmt.Printf("Ошибка при выходе: %v\n", err)
				return
			}

			fmt.Println("✅ Вы успешно вышли из системы")
			fmt.Println("Все аутентификационные данные удалены")
		},
	}
}

// В методе createListCommand обновим вывод таблицы
func (v *VSCodeExtension) createListCommand() *cobra.Command {
	var limit int
	var contestID string

	cmd := &cobra.Command{
		Use:   "list [contest_id]",
		Short: "Список отправок в контесте",
		Long: `Показать список отправок в конкретном контесте

Примеры:
  sortme list           # Отправки в текущем контесте
  sortme list 456       # Отправки в контесте 456
  sortme list --limit 5 # Последние 5 отправок
  sortme list --contest 0 # Отправки в контесте 0`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if !v.apiClient.IsAuthenticated() {
				fmt.Println("❌ Вы не аутентифицированы")
				return
			}

			// Определяем ID контеста
			targetContestID := v.config.CurrentContest
			if contestID != "" {
				targetContestID = contestID
			}
			if len(args) > 0 {
				targetContestID = args[0]
			}

			if targetContestID == "" {
				fmt.Println("❌ Не указан контест")
				fmt.Println("\n💡 Используйте:")
				fmt.Println("  sortme list 456          - отправки в контесте 456")
				fmt.Println("  sortme list --contest 0  - отправки в контесте 0")
				fmt.Println("  sortme use-contest 456   - установить контест по умолчанию")
				fmt.Println("  sortme contests          - список доступных контестов")
				return
			}

			fmt.Printf("🔍 Поиск отправок в контесте %s...\n", targetContestID)

			submissions, err := v.apiClient.GetContestSubmissions(targetContestID, limit)
			if err != nil {
				fmt.Printf("❌ Ошибка: %v\n", err)
				fmt.Println("\n💡 Проверьте:")
				fmt.Println("  - Правильность ID контеста")
				fmt.Println("  - Доступность контеста")
				fmt.Println("  - sortme contests - список контестов")
				return
			}

			if len(submissions) == 0 {
				fmt.Printf("📭 В контесте %s нет отправок\n", targetContestID)
				fmt.Println("\n💡 Попробуйте отправить решение:")
				fmt.Printf("  sortme submit файл.cpp -c %s -p ID_задачи\n", targetContestID)
				return
			}

			// Вывод таблицы отправок
			fmt.Printf("\n📊 Отправки в контесте %s (%d):\n", targetContestID, len(submissions))

			// Определяем максимальную ширину для названия задачи
			maxTaskWidth := 25
			for _, sub := range submissions {
				taskName := getTaskDisplayName(sub)
				if len(taskName) > maxTaskWidth {
					maxTaskWidth = len(taskName)
				}
			}
			if maxTaskWidth > 35 {
				maxTaskWidth = 35
			}

			// Строим таблицу
			headerFormat := "┌──────────┬─%s┬──────────┬──────────┬────────────┐\n"
			taskHeader := strings.Repeat("─", maxTaskWidth+2)
			fmt.Printf(headerFormat, taskHeader)

			fmt.Printf("│ %-8s │ %-*s │ %-8s │ %-8s │ %-10s │\n",
				"ID", maxTaskWidth, "Задача", "Статус", "Баллы", "Время")

			separatorFormat := "├──────────┼─%s┼──────────┼──────────┼────────────┤\n"
			fmt.Printf(separatorFormat, strings.Repeat("─", maxTaskWidth+2))

			for _, sub := range submissions {
				statusEmoji := getShortStatusEmoji(sub.ShownVerdict)
				statusText := getShortStatusText(sub.ShownVerdict)

				// Название задачи
				taskDisplay := getTaskDisplayName(sub)
				if len(taskDisplay) > maxTaskWidth {
					taskDisplay = taskDisplay[:maxTaskWidth-2] + ".."
				}

				points := sub.TotalPoints
				if points == 0 && sub.ShownVerdict == 1 {
					points = 100
				}

				// Время отправки
				timeDisplay := "—"
				if sub.SubmitTime != "" {
					// Пробуем распарсить время
					if t, err := time.Parse(time.RFC3339, sub.SubmitTime); err == nil {
						timeDisplay = t.Format("15:04")
					} else if len(sub.SubmitTime) >= 5 {
						timeDisplay = sub.SubmitTime[:5]
					}
				}

				fmt.Printf("│ %-8d │ %-*s │ %s %-6s │ %-8d │ %-10s │\n",
					sub.ID,
					maxTaskWidth,
					taskDisplay,
					statusEmoji,
					statusText,
					points,
					timeDisplay,
				)
			}

			footerFormat := "└──────────┴─%s┴──────────┴──────────┴────────────┘\n"
			fmt.Printf(footerFormat, strings.Repeat("─", maxTaskWidth+2))

			// Статистика
			successCount := 0
			totalPoints := 0
			for _, sub := range submissions {
				if sub.ShownVerdict == 1 {
					successCount++
				}
				totalPoints += sub.TotalPoints
			}

			fmt.Printf("\n📈 Статистика: %d/%d успешных отправок", successCount, len(submissions))
			if totalPoints > 0 {
				fmt.Printf(", всего баллов: %d", totalPoints)
			}
			fmt.Println()

			// Текущий контест
			if v.config.CurrentContest == targetContestID {
				fmt.Printf("🎯 Текущий контест: %s\n", targetContestID)
			}

			fmt.Printf("\n💡 Команды:\n")
			if len(submissions) > 0 {
				fmt.Printf("  sortme status %d      - детальная информация\n", submissions[0].ID)
			}
			fmt.Printf("  sortme use-contest %s - установить контест по умолчанию\n", targetContestID)
			fmt.Printf("  sortme problems %s    - список задач контеста\n", targetContestID)
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 0, "Ограничить количество отправок")
	cmd.Flags().StringVarP(&contestID, "contest", "c", "", "ID контеста")

	return cmd
}

// Добавим функцию для короткого текста статуса
func getShortStatusText(verdict int) string {
	switch verdict {
	case 1: // Полное решение
		return "OK"
	case 2: // Неправильный ответ
		return "WA"
	case 3: // Превышено ограничение времени
		return "TLE"
	case 4: // Превышено ограничение памяти
		return "MLE"
	case 5: // Ошибка компиляции
		return "CE"
	case 6: // Ошибка выполнения
		return "RE"
	default:
		return "??"
	}
}

// Обновим функцию для отображения имени задачи
func getTaskDisplayName(sub Submission) string {
	if sub.ProblemName != "" {
		// Сокращаем длинные названия
		name := sub.ProblemName
		if len(name) > 30 {
			name = name[:27] + "..."
		}
		return fmt.Sprintf("%d. %s", sub.ProblemID, name)
	}
	return fmt.Sprintf("%d", sub.ProblemID)
}

// В методе createProblemsCommand добавь вызов handleProblems
func (v *VSCodeExtension) createProblemsCommand() *cobra.Command {
	var contestID string

	cmd := &cobra.Command{
		Use:   "problems [contest_id]",
		Short: "Показать задачи контеста",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			// Определяем ID контеста
			targetContestID := v.config.CurrentContest
			if contestID != "" {
				targetContestID = contestID
			}
			if len(args) > 0 {
				targetContestID = args[0]
			}

			if targetContestID == "" {
				fmt.Println("❌ Не указан контест")
				fmt.Println("\n💡 Используйте:")
				fmt.Println("  sortme problems 456     - задачи контеста 456")
				fmt.Println("  sortme problems --contest 0")
				fmt.Println("  sortme use-contest 456  - установить контест по умолчанию")
				return
			}

			// ВЫЗЫВАЕМ handleProblems
			v.handleProblems(targetContestID)
		},
	}

	cmd.Flags().StringVarP(&contestID, "contest", "c", "", "ID контеста")
	return cmd
}

// В методе handleProblems изменим логику отображения статусов
func (v *VSCodeExtension) handleProblems(contestID string) {
	if !v.apiClient.IsAuthenticated() {
		fmt.Println("❌ Вы не аутентифицированы")
		return
	}

	fmt.Printf("📚 Получение списка задач для контеста %s...\n", contestID)

	contestInfo, err := v.apiClient.GetContestInfo(contestID)
	if err != nil {
		fmt.Printf("❌ Ошибка получения задач: %v\n", err)
		return
	}

	if len(contestInfo.Tasks) == 0 {
		fmt.Println("📭 Задачи не найдены")
		return
	}

	fmt.Printf("\n📚 Задачи контеста \"%s\":\n", contestInfo.Name)

	// Сначала собираем все статусы
	taskStatuses := make([]string, len(contestInfo.Tasks))
	solvedCount := 0

	for i, task := range contestInfo.Tasks {
		// Добавляем задержку чтобы избежать rate limiting
		if i > 0 {
			time.Sleep(300 * time.Millisecond)
		}

		solved, err := v.apiClient.IsTaskSolved(contestID, task.ID)
		status := "❌" // По умолчанию не решена
		if err != nil {
			status = "❓" // Неизвестно из-за ошибки
			fmt.Printf("  ⚠️  Ошибка проверки задачи %d: %v\n", task.ID, err)
		} else if solved {
			status = "✅" // Решена
			solvedCount++
		}

		taskStatuses[i] = status
	}

	// Теперь красивый вывод
	for i, task := range contestInfo.Tasks {
		status := taskStatuses[i]
		fmt.Printf("  %s %d. %s (ID: %d)\n", status, i+1, task.Name, task.ID)
	}

	fmt.Printf("\n💡 Для отправки решения используйте:\n")
	fmt.Printf("   sortme submit файл.cpp -c %s -p ID_задачи\n", contestID)

	// Статистика
	totalCount := len(contestInfo.Tasks)
	fmt.Printf("\n📊 Прогресс: %d/%d задач решено", solvedCount, totalCount)

	if totalCount > 0 {
		percent := (solvedCount * 100) / totalCount
		fmt.Printf(" (%d%%)", percent)

		// Progress bar
		barLength := 20
		filled := (solvedCount * barLength) / totalCount
		empty := barLength - filled

		fmt.Printf("\n   [")
		for i := 0; i < filled; i++ {
			fmt.Printf("█")
		}
		for i := 0; i < empty; i++ {
			fmt.Printf("░")
		}
		fmt.Printf("]")
	}
	fmt.Println()
}

func (v *VSCodeExtension) createDownloadCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "download [contest_id] [problem_id]",
		Short: "Скачать условие задачи",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			contestID := args[0]
			problemID := args[1]
			v.handleDownload(contestID, problemID)
		},
	}
}

func (v *VSCodeExtension) handleSubmit(filename, contestID, problemID, language string) {
	// Проверяем существование файла
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		fmt.Printf("❌ Файл не существует: %s\n", filename)
		return
	}

	// Проверяем аутентификацию
	if !v.apiClient.IsAuthenticated() {
		fmt.Println("❌ Вы не аутентифицированы.")
		fmt.Println("Сначала выполните аутентификацию одной из команд:")
		fmt.Println("  sortme auth      - через Telegram бота")
		fmt.Println("  sortme webauth   - через веб-сайт")
		fmt.Println("  sortme manualauth - ручной ввод")
		return
	}

	// Определяем язык если не указан
	if language == "" {
		language = v.apiClient.DetectLanguage(filename)
		if language == "unknown" {
			fmt.Println("❌ Не удалось определить язык программирования.")
			fmt.Println("Укажите явно через --language")
			fmt.Println("Доступные языки: python, java, c++, c, go, javascript, rust, typescript, php, ruby, csharp")
			return
		}
		fmt.Printf("🔍 Автоопределен язык: %s\n", language)
	} else {
		// Проверяем поддерживаемый язык
		supportedLangs := map[string]bool{
			"python": true, "java": true, "c++": true, "c": true,
			"go": true, "javascript": true, "rust": true,
			"typescript": true, "php": true, "ruby": true, "csharp": true,
		}
		if !supportedLangs[language] {
			fmt.Printf("❌ Неподдерживаемый язык: %s\n", language)
			fmt.Println("Доступные языки: python, java, c++, c, go, javascript, rust, typescript, php, ruby, csharp")
			return
		}
	}

	// Читаем исходный код
	sourceCode, err := ReadSourceCode(filename)
	if err != nil {
		fmt.Printf("❌ Ошибка чтения файла: %v\n", err)
		return
	}

	fmt.Printf("📤 Отправка решения...\n")
	fmt.Printf("📝 Файл: %s\n", filename)
	fmt.Printf("🏆 Контест: %s\n", contestID)
	fmt.Printf("📚 Задача: %s\n", problemID)
	fmt.Printf("💻 Язык: %s\n", language)
	fmt.Printf("📊 Размер кода: %d символов\n", len(sourceCode))

	// Отправляем решение
	response, err := v.apiClient.SubmitSolution(contestID, problemID, language, sourceCode)
	if err != nil {
		fmt.Printf("❌ Ошибка отправки: %v\n", err)
		fmt.Println("Проверьте:")
		fmt.Println("  - Интернет соединение")
		fmt.Println("  - Корректность contest ID и problem ID")
		fmt.Println("  - Актуальность session token (попробуйте переаутентифицироваться)")
		return
	}

	fmt.Printf("✅ Решение отправлено успешно!\n")
	fmt.Printf("🎯 ID отправки: %s\n", response.ID)
	fmt.Printf("📈 Статус: %s\n", response.Status)
	if response.Message != "" {
		fmt.Printf("💬 Сообщение: %s\n", response.Message)
	}

	fmt.Printf("\nДля проверки статуса выполните:\n")
	fmt.Printf("sortme status %s\n", response.ID)
}

func (a *APIClient) GetSubmissionStatus(submissionID string) (*SubmissionStatus, error) {
	if !a.IsAuthenticated() {
		return nil, fmt.Errorf("not authenticated")
	}

	// Сначала пробуем REST через IP
	status, err := a.tryRESTStatusViaIP(submissionID)
	if err == nil {
		return status, nil
	}

	// Если REST не работает, используем WebSocket
	fmt.Printf("🔌 Подключаемся к WebSocket для статуса %s\n", submissionID)
	return a.getStatusViaWebSocket(submissionID)
}

func (a *APIClient) tryRESTStatusViaIP(submissionID string) (*SubmissionStatus, error) {
	insecureClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}

	endpoints := []string{
		"/submission/" + submissionID,
		"/submissions/" + submissionID,
		"/api/submission/" + submissionID,
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
			var status SubmissionStatus
			if err := json.Unmarshal(body, &status); err == nil {
				return &status, nil
			}
		}
	}

	return nil, fmt.Errorf("REST статус недоступен")
}

func (v *VSCodeExtension) handleStatus(submissionID string) {
	if !v.apiClient.IsAuthenticated() {
		fmt.Println("❌ Вы не аутентифицированы")
		return
	}

	// Очищаем ID от возможного JSON формата
	cleanID := cleanSubmissionID(submissionID)
	fmt.Printf("🔍 Запрос статуса отправки %s...\n", cleanID)

	status, err := v.apiClient.GetSubmissionStatus(cleanID)
	if err != nil {
		fmt.Printf("❌ Ошибка получения статуса: %v\n", err)
		return
	}

	fmt.Printf("📊 Статус отправки %s:\n", cleanID)
	fmt.Printf("   🆔 ID: %s\n", status.ID)
	fmt.Printf("   📈 Статус: %s\n", getStatusEmoji(status.Status))

	if status.Result != "" {
		fmt.Printf("   🎯 Результат: %s\n", status.Result)
	}
	if status.Score > 0 {
		fmt.Printf("   ⭐ Баллы: %d\n", status.Score)
	}
	if status.Time != "" {
		fmt.Printf("   ⏱️  Время: %s\n", status.Time)
	}
	if status.Memory != "" {
		fmt.Printf("   💾 Память: %s\n", status.Memory)
	}

	fmt.Printf("   🌐 Подробнее: https://sort-me.org/submission/%s\n", cleanID)
}

func (a *APIClient) IsTaskSolved(contestID string, taskID int) (bool, error) {
	if !a.IsAuthenticated() {
		return false, fmt.Errorf("not authenticated")
	}

	endpoint := fmt.Sprintf("/getMySubmissionsByTask?id=%d", taskID)

	// Получаем ВСЕ отправки для задачи
	submissions, err := a.tryGetSubmissions(endpoint, 0)
	if err != nil {
		return false, err
	}

	// Проверяем ВСЕ отправки на наличие успешной
	for _, submission := range submissions {
		// Успешная отправка - вердикт 1 (Полное решение) И баллы > 0
		if submission.ShownVerdict == 1 && submission.TotalPoints > 0 {
			return true, nil
		}
		// Или если баллы = 100 (полное решение)
		if submission.TotalPoints == 100 {
			return true, nil
		}
	}

	return false, nil
}

func (v *VSCodeExtension) handleDownload(contestID, problemID string) {
	fmt.Printf("🔍 Скачивание условия задачи %s из контеста %s...\n", problemID, contestID)
	fmt.Println("⏳ Функция в разработке. Используйте sortme explore для исследования API")
}

func getStatusEmoji(status string) string {
	switch status {
	case "accepted", "AC":
		return "✅ Принято"
	case "wrong_answer", "WA":
		return "❌ Неверный ответ"
	case "time_limit_exceeded", "TLE":
		return "⏰ Превышено время"
	case "memory_limit_exceeded", "MLE":
		return "💾 Превышена память"
	case "compilation_error", "CE":
		return "🔨 Ошибка компиляции"
	case "runtime_error", "RE":
		return "💥 Ошибка выполнения"
	case "pending", "in_queue":
		return "⏳ В очереди"
	case "testing", "running":
		return "🔍 Тестируется"
	default:
		return status
	}
}

func getShortStatusEmoji(verdict int) string {
	switch verdict {
	case 1: // Полное решение
		return "✅"
	case 2: // Неправильный ответ
		return "❌"
	case 3: // Превышено ограничение времени
		return "⏰"
	case 4: // Превышено ограничение памяти
		return "💾"
	case 5: // Ошибка компиляции
		return "🔨"
	case 6: // Ошибка выполнения
		return "💥"
	case 7: // Частичное решение
		return "⚠️"
	default:
		return "⏳"
	}
}
