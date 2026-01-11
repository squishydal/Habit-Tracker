# Habit Tracker

A terminal-based habit tracking application written in Go with SQLite storage, featuring gamification elements including levels, experience points, and achievements.

## Features

### Core Functionality

**Habit Management**

- Add, delete, and track multiple habits
- Mark habits as complete for each day
- View completion history via heatmap visualization
- Automatic streak calculation

**Progress Tracking**

- Current streak tracking (consecutive days)
- Total completion count
- Best streak calculation
- Completion rate statistics
- Recent check-in history with timestamps

### Gamification System

**Experience and Levels**

- Earn 10 XP per habit completion
- Level up every 100 XP
- Bonus XP for maintaining streaks:
  - 7 day streak: +50 XP
  - 30 day streak: +200 XP
  - 100 day streak: +1000 XP
- Earn 5 coins per completion

**Achievements**

Streak Achievements:

- 3 days: 3 Day Streak
- 7 days: Week Warrior
- 30 days: Monthly Master
- 100 days: Century Champion
- 365 days: Year Legend

Completion Achievements:

- 10 completions: Getting Started
- 50 completions: Half Century
- 100 completions: Century Club
- 365 completions: Year Round
- 1000 completions: Thousand Strong

Level Achievements:

- Level 5: Blooming
- Level 10: Growing Strong
- Level 20: Habit Royalty
- Level 50: Legendary

**Visual Progression**

- Level badges that evolve with progress
- XP progress bars
- Visual indicators for different achievement tiers

### Visualization

**Heatmap View**

- GitHub-style contribution grid
- Adjustable time range (4-52 weeks in 4-week increments)
- Color-coded completion status
- Week-aligned calendar layout
- Today's date highlighted with border

## Requirements

- Go 1.16 or higher
- SQLite support (via modernc.org/sqlite, pure Go implementation)

## Installation

```bash
go get github.com/charmbracelet/bubbles/textinput
go get github.com/charmbracelet/bubbletea
go get github.com/charmbracelet/lipgloss
go get modernc.org/sqlite
go build main.go
```

## Usage

Run the application:

```bash
./main
```

The application creates a `habits.db` SQLite database file in the current directory.

### Controls

**List View**

- `Up/Down` or `k/j` - Navigate between habits
- `g` - Jump to first habit
- `G` - Jump to last habit
- `Enter` or `Space` - Toggle completion for selected habit (today)
- `a` - Add new habit
- `d` - Delete selected habit
- `h` - View heatmap for selected habit
- `q` or `Ctrl+C` - Quit

**Add Habit Mode**

- Type habit name (max 100 characters)
- `Enter` - Save habit
- `Esc` - Cancel

**Delete Confirmation**

- `y` - Confirm deletion
- `n` or `Esc` - Cancel

**Heatmap View**

- `Left/Right` - Decrease/increase weeks displayed
- `Esc`, `q`, or `h` - Return to list view

## Database Schema

**habits table**

- id: Primary key
- name: Habit name (1-100 characters)
- current_streak: Current consecutive days
- total_done: Total completions
- level: Current level (starts at 1)
- xp: Total experience points
- coins: Total coins earned
- created_at: Timestamp

**logs table**

- id: Primary key
- habit_id: Foreign key to habits
- date: Date of completion (YYYY-MM-DD)
- timestamp: Full timestamp of completion
- Unique constraint on (habit_id, date)

**achievements table**

- id: Primary key
- habit_id: Foreign key to habits
- type: Achievement type
- unlocked_at: Timestamp

## Data Integrity

- All database operations are transactional
- Foreign key constraints ensure referential integrity
- Cascade deletion removes all associated logs when habit is deleted
- Automatic recalculation of streaks and stats after each toggle
- Input validation for habit names and dates

## Statistics Calculation

**Current Streak**

- Counts consecutive days backwards from today or yesterday
- Breaks if any day is missing in the sequence

**Best Streak**

- Scans all completion history
- Identifies longest sequence of consecutive days

**Completion Rate**

- Calculated based on visible time period in heatmap view
- Shows percentage of days completed out of days displayed

## Display Features

- Color-coded interface with purple primary theme
- Progress bars for XP advancement
- Visual distinction between completed and incomplete habits
- Day labels with weekend highlighting
- Bordered container for organized layout
- Real-time feedback messages for actions

## Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) - TUI components
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Style definitions
- [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) - Pure Go SQLite driver

## Data Persistence

All habit data, completion logs, and statistics are stored in a local SQLite database file. The data persists across application sessions and can be backed up by copying the `habits.db` file.
