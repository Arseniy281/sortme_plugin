package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type VSCodeExtension struct {
	config    *Config
	apiClient *APIClient
	tgAuth    *TelegramAuth
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
		tgAuth:    NewTelegramAuth(config),
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
		v.createWebAuthCommand(),
		v.createManualAuthCommand(),
		v.createSubmitCommand(),
		v.createStatusCommand(),
		v.createWhoamiCommand(),
		v.createLogoutCommand(),
		v.createExploreCommand(),
		v.createListCommand(),
		v.createContestsCommand(),
		v.createProblemsCommand(),
		v.createDownloadCommand(),
		v.createContestInfoCommand(),
	)

	return rootCmd
}

func (v *VSCodeExtension) createAuthCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "auth",
		Short: "Аутентификация через Telegram бота",
		Run: func(cmd *cobra.Command, args []string) {
			if err := v.tgAuth.StartAuth(); err != nil {
				fmt.Printf("Ошибка аутентификации: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

func (v *VSCodeExtension) createWebAuthCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "webauth",
		Short: "Аутентификация через веб-сайт",
		Long:  "Альтернативный метод аутентификации через браузер",
		Run: func(cmd *cobra.Command, args []string) {
			if err := v.tgAuth.StartWebAuth(); err != nil {
				fmt.Printf("Ошибка аутентификации: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

func (v *VSCodeExtension) createManualAuthCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "manualauth",
		Short: "Ручная аутентификация",
		Long:  "Ручной ввод данных аутентификации",
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
				fmt.Println("Используйте одну из команд:")
				fmt.Println("  sortme auth      - через Telegram бота")
				fmt.Println("  sortme webauth   - через веб-сайт")
				fmt.Println("  sortme manualauth - ручной ввод")
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

func (v *VSCodeExtension) createExploreCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "explore",
		Short: "Инструкция по исследованию API",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("🔍 ИНСТРУКЦИЯ: Как исследовать API sort-me.org")
			fmt.Println("==============================================")
			fmt.Println()
			fmt.Println("1. 🖥️  ОТКРОЙТЕ БРАУЗЕР:")
			fmt.Println("   - Зайдите на https://sort-me.org")
			fmt.Println("   - Войдите в свой аккаунт")
			fmt.Println()
			fmt.Println("2. 🔧 ОТКРОЙТЕ ИНСТРУМЕНТЫ РАЗРАБОТЧИКА:")
			fmt.Println("   - Нажмите F12")
			fmt.Println("   - Или Ctrl+Shift+I (Windows/Linux)")
			fmt.Println("   - Или Cmd+Option+I (Mac)")
			fmt.Println()
			fmt.Println("3. 📡 ПЕРЕЙДИТЕ НА ВКЛАДКУ 'NETWORK':")
			fmt.Println("   - Нажмите на вкладку 'Network'")
			fmt.Println("   - Поставьте галочку 'Preserve log'")
			fmt.Println("   - Очистите список (кнопка 🚫)")
			fmt.Println()
			fmt.Println("4. 🚀 ОТПРАВЬТЕ РЕШЕНИЕ ЧЕРЕЗ ВЕБ-ИНТЕРФЕЙС:")
			fmt.Println("   - Выберите контест и задачу")
			fmt.Println("   - Напишите или вставьте код решения")
			fmt.Println("   - Нажмите кнопку 'Отправить'/'Submit'")
			fmt.Println()
			fmt.Println("5. 🔎 НАЙДИТЕ API ЗАПРОС:")
			fmt.Println("   - В списке запросов ищите:")
			fmt.Println("     * Метод: POST")
			fmt.Println("     * В URL есть слова: 'submit', 'solution', 'contest'")
			fmt.Println("     * Status: 200 (успешно)")
			fmt.Println()
			fmt.Println("6. 📋 СОБЕРИТЕ ИНФОРМАЦИЮ:")
			fmt.Println("   - Нажмите на найденный запрос")
			fmt.Println("   - Скопируйте:")
			fmt.Println("     а) Полный URL (вкладка Headers → General)")
			fmt.Println("     б) Headers (вкладка Headers → Request Headers)")
			fmt.Println("     в) Данные (вкладка Payload/Json)")
			fmt.Println()
			fmt.Println("7. 📝 ЗАПИШИТЕ НАЙДЕННОЕ И СООБЩИТЕ МНЕ!")
		},
	}
}

func (v *VSCodeExtension) createListCommand() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "Список последних отправок",
		Run: func(cmd *cobra.Command, args []string) {
			v.handleList(limit)
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "l", 10, "Количество отправок для показа")

	return cmd
}

func (v *VSCodeExtension) createContestsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "contests",
		Short: "Список доступных контестов",
		Run: func(cmd *cobra.Command, args []string) {
			v.handleContests()
		},
	}
}

func (v *VSCodeExtension) createProblemsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "problems [contest_id]",
		Short: "Список задач в контесте",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			contestID := args[0]
			v.handleProblems(contestID)
		},
	}
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

func (v *VSCodeExtension) createContestInfoCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "contest [contest_id]",
		Short: "Подробная информация о контесте",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			contestID := args[0]
			v.handleContestInfo(contestID)
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

func (v *VSCodeExtension) handleStatus(submissionID string) {
	if !v.apiClient.IsAuthenticated() {
		fmt.Println("❌ Вы не аутентифицированы")
		return
	}

	fmt.Printf("🔍 Запрос статуса отправки %s...\n", submissionID)

	status, err := v.apiClient.GetSubmissionStatus(submissionID)
	if err != nil {
		fmt.Printf("❌ Ошибка получения статуса: %v\n", err)
		return
	}

	fmt.Printf("📊 Статус отправки %s:\n", submissionID)
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

	fmt.Printf("   🌐 Подробнее: https://sort-me.org/submission/%s\n", submissionID)
}

func (v *VSCodeExtension) handleList(limit int) {
	if !v.apiClient.IsAuthenticated() {
		fmt.Println("❌ Вы не аутентифицированы")
		return
	}

	fmt.Printf("📋 Получение последних %d отправок...\n", limit)

	submissions, err := v.apiClient.GetSubmissions(limit)
	if err != nil {
		fmt.Printf("❌ Ошибка получения списка отправок: %v\n", err)
		fmt.Println("🔍 Попробуйте исследовать API с помощью: sortme explore")
		return
	}

	if len(submissions) == 0 {
		fmt.Println("📭 Отправки не найдены")
		return
	}

	fmt.Printf("\n📊 Последние %d отправок (задача 2472, контест 456):\n", len(submissions))
	fmt.Println("┌──────────┬────────────┬────────┬──────────────┐")
	fmt.Println("│    ID    │   Статус   │ Баллы  │    Детали    │")
	fmt.Println("├──────────┼────────────┼────────┼──────────────┤")

	for _, sub := range submissions {
		statusEmoji := getShortStatusEmoji(sub.ShownVerdict)
		statusText := getStatusText(sub.ShownVerdict)

		details := ""
		if sub.ShownTest > 0 {
			details = fmt.Sprintf("Тест %d", sub.ShownTest)
		}

		fmt.Printf("│ %-8d │ %-2s %-8s │ %-6d │ %-12s │\n",
			sub.ID,
			statusEmoji,
			statusText,
			sub.TotalPoints,
			details,
		)
	}
	fmt.Println("└──────────┴────────────┴────────┴──────────────┘")

	// Показываем ссылки для детального просмотра
	fmt.Println("\n🔍 Для детальной информации используйте:")
	for i, sub := range submissions {
		if i < 3 { // Показываем только первые 3
			fmt.Printf("  sortme status %d\n", sub.ID)
		}
	}
}

func (v *VSCodeExtension) handleContests() {
	if !v.apiClient.IsAuthenticated() {
		fmt.Println("❌ Вы не аутентифицированы")
		return
	}

	// Пока используем известные контесты, позже найдем endpoint для списка
	fmt.Println("🏆 Известные контесты:")
	fmt.Println("  🚀 456 - Лабораторная АиСД ИТМО №2 (25/26)")
	fmt.Println("  💡 Для просмотра задач используйте: sortme problems 456")
	fmt.Println("\n🔍 Чтобы найти другие контесты, используйте sortme explore")
}

func (a *APIClient) IsTaskSolved(contestID string, taskID int) (bool, error) {
	if !a.IsAuthenticated() {
		return false, fmt.Errorf("not authenticated")
	}

	endpoint := fmt.Sprintf("/getMySubmissionsByTask?id=%d&contestid=%s", taskID, contestID)

	submissions, err := a.tryGetSubmissions(endpoint)
	if err != nil {
		return false, err
	}

	// Если есть отправки, проверяем последнюю
	if len(submissions) > 0 {
		lastSubmission := submissions[0]
		return lastSubmission.ShownVerdict == 1 && lastSubmission.TotalPoints == 100, nil
	}

	return false, nil
}

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

	for i, task := range contestInfo.Tasks {
		// Добавляем задержку чтобы избежать rate limiting
		if i > 0 {
			time.Sleep(500 * time.Millisecond)
		}

		solved, err := v.apiClient.IsTaskSolved(contestID, task.ID)
		status := "🔓"
		if err == nil && solved {
			status = "✅"
		} else if err != nil {
			status = "❓" // Неизвестно из-за ошибки
		}

		taskStatuses[i] = status
	}

	// Теперь красивый вывод
	for i, task := range contestInfo.Tasks {
		fmt.Printf("  %s %d. %s (ID: %d)\n", taskStatuses[i], i+1, task.Name, task.ID)
	}

	fmt.Printf("\n💡 Для отправки решения используйте:\n")
	fmt.Printf("   sortme submit файл.cpp -c %s -p ID_задачи\n", contestID)

	// Статистика
	solvedCount := 0
	for _, status := range taskStatuses {
		if status == "✅" {
			solvedCount++
		}
	}
	fmt.Printf("\n📊 Прогресс: %d/%d задач решено\n", solvedCount, len(contestInfo.Tasks))
}

func (v *VSCodeExtension) handleDownload(contestID, problemID string) {
	fmt.Printf("🔍 Скачивание условия задачи %s из контеста %s...\n", problemID, contestID)
	fmt.Println("⏳ Функция в разработке. Используйте sortme explore для исследования API")
}

func (v *VSCodeExtension) handleContestInfo(contestID string) {
	if !v.apiClient.IsAuthenticated() {
		fmt.Println("❌ Вы не аутентифицированы")
		return
	}

	fmt.Printf("🏆 Получение информации о контесте %s...\n", contestID)

	contestInfo, err := v.apiClient.GetContestInfo(contestID)
	if err != nil {
		fmt.Printf("❌ Ошибка: %v\n", err)
		return
	}

	fmt.Printf("\n🏆 Контест: %s\n", contestInfo.Name)
	fmt.Printf("   🆔 ID: %d\n", contestInfo.ID)
	fmt.Printf("   📊 Статус: %s\n", getContestStatus(contestInfo.Starts, contestInfo.Ends))
	fmt.Printf("   🗓️  Начало: %s\n", formatTime(contestInfo.Starts))
	fmt.Printf("   🗓️  Окончание: %s\n", formatTime(contestInfo.Ends))

	if contestInfo.Registered {
		fmt.Printf("   ✅ Вы зарегистрированы\n")
	} else {
		fmt.Printf("   ❌ Вы не зарегистрированы\n")
	}

	fmt.Printf("\n📚 Задачи (%d):\n", len(contestInfo.Tasks))
	for i, task := range contestInfo.Tasks {
		fmt.Printf("   %d. %s (ID: %d)\n", i+1, task.Name, task.ID)
	}

	fmt.Printf("\n🚀 Команды для работы:\n")
	fmt.Printf("   sortme problems %s    - список задач\n", contestID)
	fmt.Printf("   sortme submit файл.cpp -c %s -p ID_задачи - отправить решение\n", contestID)
}

// Вспомогательные функции для форматирования
func formatTime(timestamp int64) string {
	if timestamp == 0 {
		return "не указано"
	}
	t := time.Unix(timestamp, 0)
	return t.Format("02.01.2006 15:04")
}

func getContestStatus(start, end int64) string {
	now := time.Now().Unix()

	if now < start {
		return "🔜 Предстоит"
	} else if now > end {
		return "🔚 Завершен"
	} else {
		return "🚀 Идет сейчас"
	}
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
	case 5: // Ошибка компиляции
		return "🔨"
	default:
		return "⏳"
	}
}

func getStatusText(verdict int) string {
	switch verdict {
	case 1:
		return "Принято"
	case 2:
		return "Неверный"
	case 5:
		return "Компиляция"
	default:
		return "В процессе"
	}
}
