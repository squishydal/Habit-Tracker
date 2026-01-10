package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	_ "modernc.org/sqlite"
)

// ============================================================
// CONSTANTS
// ============================================================

const (
	weeksStep     = 4
	minWeeks      = 4
	maxWeeks      = 52
	maxHabitName  = 100
	minHabitName  = 1
	maxLogDays    = 365
	recentLogDays = 14
	maxRecentShow = 7
)

// ============================================================
// DATABASE
// ============================================================

type Database struct {
	db *sql.DB
}

type Habit struct {
	ID            int
	Name          string
	CurrentStreak int
	TotalDone     int
	CreatedAt     string
	Level         int
	XP            int
	Coins         int
}

type LogEntry struct {
	Date      string
	Timestamp string
}

func NewDatabase() (*Database, error) {
	db, err := sql.Open("sqlite", "./habits.db")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	schema := `
		CREATE TABLE IF NOT EXISTS habits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL CHECK(length(trim(name)) > 0),
			current_streak INTEGER DEFAULT 0 CHECK(current_streak >= 0),
			total_done INTEGER DEFAULT 0 CHECK(total_done >= 0),
			level INTEGER DEFAULT 1 CHECK(level >= 1),
			xp INTEGER DEFAULT 0 CHECK(xp >= 0),
			coins INTEGER DEFAULT 0 CHECK(coins >= 0),
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			habit_id INTEGER NOT NULL,
			date TEXT NOT NULL,
			timestamp TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(habit_id, date),
			FOREIGN KEY (habit_id) REFERENCES habits(id) ON DELETE CASCADE
		);

		CREATE TABLE IF NOT EXISTS achievements (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			habit_id INTEGER NOT NULL,
			type TEXT NOT NULL,
			unlocked_at TEXT DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (habit_id) REFERENCES habits(id) ON DELETE CASCADE
		);

		CREATE INDEX IF NOT EXISTS idx_logs_habit_date ON logs(habit_id, date);
		CREATE INDEX IF NOT EXISTS idx_logs_date ON logs(date);
	`

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return &Database{db: db}, nil
}

func (d *Database) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *Database) AddHabit(name string) error {
	name = strings.TrimSpace(name)

	if len(name) < minHabitName {
		return fmt.Errorf("habit name cannot be empty")
	}

	if len(name) > maxHabitName {
		return fmt.Errorf("habit name too long (max %d characters)", maxHabitName)
	}

	_, err := d.db.Exec("INSERT INTO habits (name) VALUES (?)", name)
	if err != nil {
		return fmt.Errorf("failed to add habit: %w", err)
	}

	return nil
}

func (d *Database) GetHabits() ([]Habit, error) {
	rows, err := d.db.Query(`
		SELECT id, name, current_streak, total_done, 
		       COALESCE(level, 1), COALESCE(xp, 0), COALESCE(coins, 0), created_at 
		FROM habits ORDER BY id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get habits: %w", err)
	}
	defer rows.Close()

	var habits []Habit
	for rows.Next() {
		var h Habit
		if err := rows.Scan(&h.ID, &h.Name, &h.CurrentStreak, &h.TotalDone,
			&h.Level, &h.XP, &h.Coins, &h.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan habit: %w", err)
		}
		habits = append(habits, h)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating habits: %w", err)
	}

	return habits, nil
}

func (d *Database) DeleteHabit(id int) error {
	result, err := d.db.Exec("DELETE FROM habits WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("failed to delete habit: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("habit not found")
	}

	return nil
}

func (d *Database) ToggleHabit(habitID int, date string) (bool, error) {
	// Validate date format
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return false, fmt.Errorf("invalid date format: %w", err)
	}

	// Check if already logged
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM logs WHERE habit_id = ? AND date = ?", habitID, date).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check log status: %w", err)
	}

	tx, err := d.db.Begin()
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	isDone := false
	if count > 0 {
		// Remove log
		_, err = tx.Exec("DELETE FROM logs WHERE habit_id = ? AND date = ?", habitID, date)
		if err != nil {
			return false, fmt.Errorf("failed to remove log: %w", err)
		}
	} else {
		// Add log with current timestamp
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		_, err = tx.Exec("INSERT INTO logs (habit_id, date, timestamp) VALUES (?, ?, ?)", habitID, date, timestamp)
		if err != nil {
			return false, fmt.Errorf("failed to add log: %w", err)
		}
		isDone = true
	}

	// Recalculate stats
	if err := d.recalculateStats(tx, habitID); err != nil {
		return false, fmt.Errorf("failed to recalculate stats: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return isDone, nil
}

func (d *Database) recalculateStats(tx *sql.Tx, habitID int) error {
	rows, err := tx.Query(`
		SELECT date FROM logs 
		WHERE habit_id = ? 
		ORDER BY date DESC
	`, habitID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			return err
		}
		dates = append(dates, date)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	// Calculate current streak
	streak := 0
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	for i, dateStr := range dates {
		if i == 0 {
			// First entry must be today or yesterday to start streak
			if dateStr != today && dateStr != yesterday {
				break
			}
			streak = 1
			continue
		}

		prevDate, err := time.Parse("2006-01-02", dates[i-1])
		if err != nil {
			return fmt.Errorf("failed to parse previous date: %w", err)
		}

		currDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			return fmt.Errorf("failed to parse current date: %w", err)
		}

		diff := int(prevDate.Sub(currDate).Hours() / 24)

		if diff == 1 {
			streak++
		} else {
			break
		}
	}

	// Calculate XP and level based on total completions
	totalDone := len(dates)
	xp := totalDone * 10    // 10 XP per completion
	level := 1 + (xp / 100) // Level up every 100 XP
	coins := totalDone * 5  // 5 coins per completion

	// Bonus XP for streaks
	if streak >= 7 {
		xp += 50 // Weekly streak bonus
	}
	if streak >= 30 {
		xp += 200 // Monthly streak bonus
	}
	if streak >= 100 {
		xp += 1000 // Epic streak bonus
	}

	_, err = tx.Exec(`
		UPDATE habits 
		SET current_streak = ?, total_done = ?, level = ?, xp = ?, coins = ?
		WHERE id = ?
	`, streak, totalDone, level, xp, coins, habitID)

	return err
}

func (d *Database) GetLogs(habitID int, days int) (map[string]bool, error) {
	if days < 0 {
		return nil, fmt.Errorf("days must be non-negative")
	}

	rows, err := d.db.Query(`
		SELECT date FROM logs 
		WHERE habit_id = ?
		AND date >= date('now', '-' || ? || ' days')
	`, habitID, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs: %w", err)
	}
	defer rows.Close()

	logs := make(map[string]bool)
	for rows.Next() {
		var date string
		if err := rows.Scan(&date); err != nil {
			return nil, fmt.Errorf("failed to scan log: %w", err)
		}
		logs[date] = true
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating logs: %w", err)
	}

	return logs, nil
}

func (d *Database) GetLogsWithTime(habitID int, days int) (map[string]LogEntry, error) {
	if days < 0 {
		return nil, fmt.Errorf("days must be non-negative")
	}

	rows, err := d.db.Query(`
		SELECT date, timestamp FROM logs 
		WHERE habit_id = ?
		AND date >= date('now', '-' || ? || ' days')
		ORDER BY date DESC
	`, habitID, days)
	if err != nil {
		return nil, fmt.Errorf("failed to get logs with time: %w", err)
	}
	defer rows.Close()

	logs := make(map[string]LogEntry)
	for rows.Next() {
		var entry LogEntry
		if err := rows.Scan(&entry.Date, &entry.Timestamp); err != nil {
			return nil, fmt.Errorf("failed to scan log entry: %w", err)
		}
		logs[entry.Date] = entry
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating log entries: %w", err)
	}

	return logs, nil
}

// ============================================================
// STYLES
// ============================================================

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4")).Padding(0, 1)
	subtitleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4")).Background(lipgloss.Color("#3C3C3C")).Padding(0, 1)
	normalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FAFAFA"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("#626262"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#39D353")).Bold(true)
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F87")).Bold(true)
	streakStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Bold(true)
	warningStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500"))

	// Heatmap colors (GitHub-style)
	colorNone   = lipgloss.Color("#161B22")
	colorLevel1 = lipgloss.Color("#0E4429")
	colorLevel2 = lipgloss.Color("#006D32")
	colorLevel3 = lipgloss.Color("#26A641")
	colorLevel4 = lipgloss.Color("#39D353")

	boxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 2)
)

// ============================================================
// MODEL
// ============================================================

type mode int

const (
	modeList mode = iota
	modeAdd
	modeDelete
	modeHeatmap
)

type Model struct {
	db           *Database
	habits       []Habit
	cursor       int
	mode         mode
	input        textinput.Model
	message      string
	messageType  string // "success", "error", "info"
	logs         map[string]bool
	logsWithTime map[string]LogEntry
	weeks        int
	width        int
	height       int
	err          error
}

func NewModel() (*Model, error) {
	db, err := NewDatabase()
	if err != nil {
		return nil, err
	}

	habits, err := db.GetHabits()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to load habits: %w", err)
	}

	input := textinput.New()
	input.Placeholder = "Enter habit name..."
	input.Width = 50
	input.CharLimit = maxHabitName

	return &Model{
		db:          db,
		habits:      habits,
		mode:        modeList,
		input:       input,
		weeks:       12,
		messageType: "info",
	}, nil
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) setMessage(msg string, msgType string) {
	m.message = msg
	m.messageType = msgType
}

func (m *Model) setError(err error) {
	if err != nil {
		m.setMessage("❌ "+err.Error(), "error")
		m.err = err
	}
}

// ============================================================
// UPDATE
// ============================================================

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

		switch m.mode {
		case modeList:
			return m.updateList(msg)
		case modeAdd:
			return m.updateAdd(msg)
		case modeDelete:
			return m.updateDelete(msg)
		case modeHeatmap:
			return m.updateHeatmap(msg)
		}
	}

	return m, nil
}

func (m *Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.message = ""
	m.err = nil

	switch msg.String() {
	case "q":
		return m, tea.Quit

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.habits)-1 {
			m.cursor++
		}

	case "g":
		m.cursor = 0

	case "G":
		if len(m.habits) > 0 {
			m.cursor = len(m.habits) - 1
		}

	case "a":
		m.mode = modeAdd
		m.input.SetValue("")
		m.input.Focus()

	case "d":
		if len(m.habits) > 0 {
			m.mode = modeDelete
		} else {
			m.setMessage("No habits to delete", "info")
		}

	case "enter", " ":
		if len(m.habits) == 0 {
			m.setMessage("No habits yet. Press 'a' to add one!", "info")
			break
		}

		today := time.Now().Format("2006-01-02")
		isDone, err := m.db.ToggleHabit(m.habits[m.cursor].ID, today)
		if err != nil {
			m.setError(err)
		} else {
			if err := m.refresh(); err != nil {
				m.setError(err)
			} else {
				if isDone {
					m.setMessage("✓ Marked as done!", "success")
				} else {
					m.setMessage("○ Unmarked", "info")
				}
			}
		}

	case "h":
		if len(m.habits) == 0 {
			m.setMessage("No habits to view", "info")
			break
		}

		logs, err := m.db.GetLogs(m.habits[m.cursor].ID, maxLogDays)
		if err != nil {
			m.setError(err)
			break
		}

		logsWithTime, err := m.db.GetLogsWithTime(m.habits[m.cursor].ID, maxLogDays)
		if err != nil {
			m.setError(err)
			break
		}

		m.logs = logs
		m.logsWithTime = logsWithTime
		m.mode = modeHeatmap
	}

	return m, nil
}

func (m *Model) updateAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeList
		m.input.Blur()
		return m, nil

	case "enter":
		name := strings.TrimSpace(m.input.Value())
		if name == "" {
			m.setMessage("Habit name cannot be empty", "error")
			return m, nil
		}

		if err := m.db.AddHabit(name); err != nil {
			m.setError(err)
		} else {
			if err := m.refresh(); err != nil {
				m.setError(err)
			} else {
				m.setMessage("✓ Habit added!", "success")
				// Move cursor to the new habit (last in list)
				if len(m.habits) > 0 {
					m.cursor = len(m.habits) - 1
				}
			}
		}

		m.mode = modeList
		m.input.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) updateDelete(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if len(m.habits) == 0 {
			m.mode = modeList
			return m, nil
		}

		if err := m.db.DeleteHabit(m.habits[m.cursor].ID); err != nil {
			m.setError(err)
		} else {
			if err := m.refresh(); err != nil {
				m.setError(err)
			} else {
				// Adjust cursor if needed
				if m.cursor >= len(m.habits) && m.cursor > 0 {
					m.cursor--
				}
				m.setMessage("✓ Habit deleted", "success")
			}
		}
		m.mode = modeList

	case "n", "N", "esc":
		m.mode = modeList
	}

	return m, nil
}

func (m *Model) updateHeatmap(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "h":
		m.mode = modeList

	case "left":
		if m.weeks > minWeeks {
			m.weeks -= weeksStep
		}

	case "right":
		if m.weeks < maxWeeks {
			m.weeks += weeksStep
		}
	}

	return m, nil
}

func (m *Model) refresh() error {
	habits, err := m.db.GetHabits()
	if err != nil {
		return err
	}
	m.habits = habits
	return nil
}

// ============================================================
// VIEW
// ============================================================

// Get achievements for a habit
func (m *Model) getAchievements(habit Habit) []string {
	var achievements []string

	// Streak achievements
	if habit.CurrentStreak >= 3 {
		achievements = append(achievements, "🔥 3 Day Streak!")
	}
	if habit.CurrentStreak >= 7 {
		achievements = append(achievements, "⭐ Week Warrior!")
	}
	if habit.CurrentStreak >= 30 {
		achievements = append(achievements, "🏆 Monthly Master!")
	}
	if habit.CurrentStreak >= 100 {
		achievements = append(achievements, "👑 Century Champion!")
	}
	if habit.CurrentStreak >= 365 {
		achievements = append(achievements, "💎 Year Legend!")
	}

	// Completion achievements
	if habit.TotalDone >= 10 {
		achievements = append(achievements, "✨ Getting Started (10)")
	}
	if habit.TotalDone >= 50 {
		achievements = append(achievements, "🎯 Half Century (50)")
	}
	if habit.TotalDone >= 100 {
		achievements = append(achievements, "💪 Century Club (100)")
	}
	if habit.TotalDone >= 365 {
		achievements = append(achievements, "🌟 Year Round (365)")
	}
	if habit.TotalDone >= 1000 {
		achievements = append(achievements, "🚀 Thousand Strong (1000)")
	}

	// Level achievements
	if habit.Level >= 5 {
		achievements = append(achievements, "🌻 Blooming (Level 5)")
	}
	if habit.Level >= 10 {
		achievements = append(achievements, "🌳 Growing Strong (Level 10)")
	}
	if habit.Level >= 20 {
		achievements = append(achievements, "👑 Habit Royalty (Level 20)")
	}
	if habit.Level >= 50 {
		achievements = append(achievements, "🔥 Legendary (Level 50)")
	}

	return achievements
}

func (m *Model) View() string {
	var content string

	switch m.mode {
	case modeList:
		content = m.viewList()
	case modeAdd:
		content = m.viewAdd()
	case modeDelete:
		content = m.viewDelete()
	case modeHeatmap:
		content = m.viewHeatmap()
	}

	if m.message != "" {
		msgStyle := dimStyle
		switch m.messageType {
		case "success":
			msgStyle = successStyle
		case "error":
			msgStyle = errorStyle
		case "info":
			msgStyle = warningStyle
		}
		content += "\n\n" + msgStyle.Render(m.message)
	}

	return boxStyle.Render(content)
}

func (m *Model) viewList() string {
	var s strings.Builder

	s.WriteString(titleStyle.Render("⚡️  HABIT TRACKER  ⚡️") + "\n\n")

	if len(m.habits) == 0 {
		s.WriteString(dimStyle.Render("No habits yet. Press 'a' to add your first habit!\n"))
	} else {
		for i, habit := range m.habits {
			cursor := "  "
			style := normalStyle

			if i == m.cursor {
				cursor = "› "
				style = selectedStyle
			}

			// Check if done today
			today := time.Now().Format("2006-01-02")
			isDone := false
			if logs, err := m.db.GetLogs(habit.ID, 1); err == nil {
				isDone = logs[today]
			}

			status := "○"
			if isDone {
				status = "✓"
			}

			// Level badge
			levelBadge := m.getLevelBadge(habit.Level)

			// XP progress bar
			xpInLevel := habit.XP % 100
			xpBar := m.getProgressBar(xpInLevel, 100, 10)

			line := fmt.Sprintf("%s%s %s %s", cursor, status, habit.Name, levelBadge)
			streakInfo := fmt.Sprintf("  [🔥 %d | 💎 %d coins]", habit.CurrentStreak, habit.Coins)

			s.WriteString(style.Render(line))
			if i == m.cursor {
				s.WriteString(streakStyle.Render(streakInfo))
				s.WriteString("\n")
				s.WriteString(dimStyle.Render(fmt.Sprintf("     %s %d/%d XP", xpBar, xpInLevel, 100)))
			} else {
				s.WriteString(dimStyle.Render(streakInfo))
			}
			s.WriteString("\n")
		}
	}

	s.WriteString("\n")
	s.WriteString(dimStyle.Render("↑/↓: navigate | enter: toggle | a: add | d: delete | h: heatmap | q: quit"))

	return s.String()
}

func (m *Model) getLevelBadge(level int) string {
	badges := map[int]string{
		1:  "🌱", // Seedling
		2:  "🌿", // Herb
		3:  "🍀", // Clover
		5:  "🌻", // Sunflower
		10: "🌳", // Tree
		15: "🏆", // Trophy
		20: "👑", // Crown
		25: "⭐", // Star
		30: "💫", // Dizzy
		50: "🔥", // Fire
	}

	badge := "🌱"
	for lvl, b := range badges {
		if level >= lvl {
			badge = b
		}
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFA500")).
		Render(fmt.Sprintf("[Lv.%d %s]", level, badge))
}

func (m *Model) getProgressBar(current, max, width int) string {
	percentage := float64(current) / float64(max)
	filled := int(percentage * float64(width))

	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}

	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#7D56F4")).
		Render(bar)
}

func (m *Model) viewAdd() string {
	var s strings.Builder

	s.WriteString(titleStyle.Render("Add New Habit") + "\n\n")
	s.WriteString(m.input.View() + "\n\n")
	s.WriteString(dimStyle.Render("enter: save | esc: cancel"))

	return s.String()
}

func (m *Model) viewDelete() string {
	if len(m.habits) == 0 {
		return ""
	}

	var s strings.Builder

	s.WriteString(errorStyle.Render("⚠  Delete Habit?") + "\n\n")
	s.WriteString(fmt.Sprintf("Are you sure you want to delete '%s'?\n", m.habits[m.cursor].Name))
	s.WriteString(dimStyle.Render("This will remove all history for this habit.\n\n"))
	s.WriteString(dimStyle.Render("y: yes | n: no"))

	return s.String()
}

func (m *Model) viewHeatmap() string {
	if len(m.habits) == 0 {
		return ""
	}

	var s strings.Builder
	habit := m.habits[m.cursor]

	// Header with habit name and streak
	headerBox := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#7D56F4")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(0, 2).
		MarginBottom(1)

	headerContent := fmt.Sprintf("📊 %s  %s",
		habit.Name,
		streakStyle.Render(fmt.Sprintf("🔥 %d day streak", habit.CurrentStreak)))

	s.WriteString(headerBox.Render(headerContent) + "\n\n")

	// Generate heatmap with proper date alignment
	endDate := time.Now()
	startDate := endDate.AddDate(0, 0, -(m.weeks*7)+1)

	// Adjust start to Sunday
	for startDate.Weekday() != time.Sunday {
		startDate = startDate.AddDate(0, 0, -1)
	}

	// Calculate total days to display
	totalDays := int(endDate.Sub(startDate).Hours()/24) + 1
	numWeeks := (totalDays + 6) / 7

	// Build heatmap section
	heatmapBox := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#3C3C3C")).
		Padding(1, 2)

	var heatmap strings.Builder

	// Day labels and heatmap grid
	days := []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}
	dayColors := []lipgloss.Color{
		lipgloss.Color("#888888"), // Sun
		lipgloss.Color("#AAAAAA"), // Mon
		lipgloss.Color("#AAAAAA"), // Tue
		lipgloss.Color("#AAAAAA"), // Wed
		lipgloss.Color("#AAAAAA"), // Thu
		lipgloss.Color("#AAAAAA"), // Fri
		lipgloss.Color("#888888"), // Sat
	}

	for day := 0; day < 7; day++ {
		// Day label with color
		dayLabel := lipgloss.NewStyle().
			Foreground(dayColors[day]).
			Bold(true).
			Width(6).
			Render(days[day])
		heatmap.WriteString(dayLabel + "  ")

		// Squares for each week
		for week := 0; week < numWeeks; week++ {
			date := startDate.AddDate(0, 0, week*7+day)

			if date.Before(startDate) || date.After(endDate) {
				// Empty space for dates outside range
				heatmap.WriteString("    ")
				continue
			}

			dateStr := date.Format("2006-01-02")
			color := colorNone
			symbol := "  "

			if m.logs[dateStr] {
				color = colorLevel4
				symbol = "██"
			} else {
				symbol = "░░"
			}

			// Add border for today
			if dateStr == time.Now().Format("2006-01-02") {
				cell := lipgloss.NewStyle().
					Foreground(color).
					Bold(true).
					Render("[" + symbol + "]")
				heatmap.WriteString(cell)
			} else {
				cell := lipgloss.NewStyle().
					Foreground(color).
					Render(" " + symbol + " ")
				heatmap.WriteString(cell)
			}
		}
		heatmap.WriteString("\n")
	}

	s.WriteString(heatmapBox.Render(heatmap.String()) + "\n\n")

	// Stats section in a nice grid
	statsBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Width(50)

	// Calculate completion rate for visible period
	daysShown := 0
	daysCompleted := 0
	for i := 0; i < totalDays; i++ {
		checkDate := startDate.AddDate(0, 0, i)
		if !checkDate.After(endDate) {
			daysShown++
			dateStr := checkDate.Format("2006-01-02")
			if m.logs[dateStr] {
				daysCompleted++
			}
		}
	}

	completionRate := 0.0
	if daysShown > 0 {
		completionRate = float64(daysCompleted) / float64(daysShown) * 100
	}

	var stats strings.Builder
	stats.WriteString(subtitleStyle.Render("📈 Statistics") + "\n\n")

	// Create stat rows
	statRow := func(label, value, color string) string {
		labelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA")).
			Width(20).
			Render(label)

		valueStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(color)).
			Bold(true).
			Render(value)

		return labelStyle + valueStyle
	}

	// Calculate XP in current level
	xpInLevel := habit.XP % 100
	xpToNext := 100 - xpInLevel

	stats.WriteString(statRow("Level:", fmt.Sprintf("%d %s", habit.Level, m.getLevelBadge(habit.Level)), "#FFA500") + "\n")
	stats.WriteString(statRow("Experience:", fmt.Sprintf("%d XP (%d to next)", habit.XP, xpToNext), "#7D56F4") + "\n")
	stats.WriteString(statRow("Coins:", fmt.Sprintf("%d 💎", habit.Coins), "#FFD700") + "\n\n")
	stats.WriteString(statRow("Current Streak:", fmt.Sprintf("%d days", habit.CurrentStreak), "#FFA500") + "\n")
	stats.WriteString(statRow("Total Completions:", fmt.Sprintf("%d times", habit.TotalDone), "#39D353") + "\n")
	stats.WriteString(statRow("Completion Rate:", fmt.Sprintf("%.1f%%", completionRate), "#7D56F4") + "\n")
	stats.WriteString(statRow("Period Shown:", fmt.Sprintf("%d days", daysShown), "#626262") + "\n")

	// Best streak calculation
	bestStreak := m.calculateBestStreak(m.logs)
	stats.WriteString(statRow("Best Streak:", fmt.Sprintf("%d days", bestStreak), "#FF6B6B") + "\n\n")

	// Achievements
	stats.WriteString(subtitleStyle.Render("🏆 Achievements") + "\n")
	achievements := m.getAchievements(habit)
	if len(achievements) > 0 {
		for _, ach := range achievements {
			stats.WriteString("  " + successStyle.Render(ach) + "\n")
		}
	} else {
		stats.WriteString(dimStyle.Render("  Keep going to unlock achievements!\n"))
	}

	s.WriteString(statsBox.Render(stats.String()) + "\n\n")

	// Recent check-ins in a cleaner format
	recentBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Padding(1, 2).
		Width(50)

	var recent strings.Builder
	recent.WriteString(subtitleStyle.Render("⏱️  Recent Check-ins") + "\n\n")

	count := 0
	for i := 0; i < recentLogDays && count < maxRecentShow; i++ {
		checkDate := endDate.AddDate(0, 0, -i)
		dateStr := checkDate.Format("2006-01-02")

		if entry, exists := m.logsWithTime[dateStr]; exists {
			timestamp, err := time.Parse("2006-01-02 15:04:05", entry.Timestamp)
			if err != nil {
				continue
			}

			timeStr := timestamp.Format("3:04 PM")
			dateDisplay := checkDate.Format("Mon, Jan 2")

			// Calculate days ago
			daysAgo := int(time.Now().Sub(checkDate).Hours() / 24)
			daysAgoStr := ""
			if daysAgo == 0 {
				daysAgoStr = "Today"
			} else if daysAgo == 1 {
				daysAgoStr = "Yesterday"
			} else {
				daysAgoStr = fmt.Sprintf("%d days ago", daysAgo)
			}

			recent.WriteString(fmt.Sprintf("%s  %s  %s\n",
				successStyle.Render("✓"),
				lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA")).Width(15).Render(dateDisplay),
				lipgloss.NewStyle().Foreground(lipgloss.Color("#626262")).Render(timeStr+" • "+daysAgoStr)))
			count++
		}
	}

	if count == 0 {
		recent.WriteString(dimStyle.Render("  No recent check-ins in the last 7 days\n"))
	}

	s.WriteString(recentBox.Render(recent.String()) + "\n\n")

	// Legend
	legendBox := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#626262")).
		Padding(0, 1)

	legend := fmt.Sprintf("Legend:  %s No activity   %s Completed   [██] Today     Showing %d weeks",
		lipgloss.NewStyle().Foreground(colorNone).Render("░░"),
		lipgloss.NewStyle().Foreground(colorLevel4).Render("██"),
		m.weeks)

	s.WriteString(legendBox.Render(legend) + "\n\n")

	// Controls
	s.WriteString(dimStyle.Render("←/→: adjust weeks (±4) | esc/h/q: back to list"))

	return s.String()
}

// Helper function to calculate best streak
func (m *Model) calculateBestStreak(logs map[string]bool) int {
	if len(logs) == 0 {
		return 0
	}

	// Get all dates and sort them
	var dates []time.Time
	for dateStr := range logs {
		date, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		dates = append(dates, date)
	}

	if len(dates) == 0 {
		return 0
	}

	// Sort dates
	for i := 0; i < len(dates)-1; i++ {
		for j := i + 1; j < len(dates); j++ {
			if dates[j].Before(dates[i]) {
				dates[i], dates[j] = dates[j], dates[i]
			}
		}
	}

	bestStreak := 1
	currentStreak := 1

	for i := 1; i < len(dates); i++ {
		diff := int(dates[i].Sub(dates[i-1]).Hours() / 24)
		if diff == 1 {
			currentStreak++
			if currentStreak > bestStreak {
				bestStreak = currentStreak
			}
		} else {
			currentStreak = 1
		}
	}

	return bestStreak
}

// ============================================================
// MAIN
// ============================================================

func main() {
	m, err := NewModel()
	if err != nil {
		fmt.Printf("Error initializing: %v\n", err)
		os.Exit(1)
	}
	defer m.db.Close()

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}
}
