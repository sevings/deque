# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`deque` is a Telegram bot that schedules and sends questions to a configured chat at specified times. It uses SQLite for persistence and maintains a scheduler to trigger questions at the right time.

## Development Commands

### Building and Running
```bash
go build -o deque
./deque
```

### Testing
```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/deque_test/

# Run a specific test
go test ./internal/deque_test/ -run TestDeque_ScheduleQuestions

# Run tests with verbose output
go test -v ./...
```

### Configuration
The application reads configuration from a TOML file. Set the `DEQUE_CONFIG` environment variable to specify a custom config path, otherwise it defaults to `deque.toml`.

Required configuration fields (see `deque.sample.toml`):
- `tg_token`: Telegram bot token (required)
- `db_path`: Path to SQLite database file
- `admin_ids`: Array of Telegram user IDs with admin access
- `chat_id`: Target chat ID where questions will be sent
- `location`: Timezone (e.g., "Europe/Moscow")
- `default_time`: Default time for questions without explicit time (format: "HH:MM")
- `max_jobs`: Maximum number of scheduled jobs
- `format`: Optional format string for questions (uses `%s` for content)

## Architecture

### Core Components

**Main Flow** (`main.go`):
1. Load configuration (`deque.LoadConfig()`)
2. Initialize logger (zap)
3. Load database with timezone
4. Create scheduler (`deque.NewSched`)
5. Create Deque instance (business logic)
6. Create Bot instance (Telegram interface)
7. Start all components and wait for interrupt signal

**Deque** (`internal/deque/deque.go`):
- Core business logic for scheduling questions
- Parses question input with flexible date/time formats
- Manages the lifecycle of scheduled questions
- Coordinates between DB and Scheduler

**Bot** (`internal/deque/bot.go`):
- Telegram bot interface using telebot.v3
- Admin-only access control via `admin_ids` config
- Commands: `/start`, `/help`, `/list`, `/stat`
- Text messages are parsed as question schedules
- `AskQuestion()` sends scheduled questions to the target chat

**Scheduler** (`internal/deque/sched.go`):
- Time-based job scheduler using Go timers
- Thread-safe with mutex protection
- Can schedule, cancel, and execute jobs by ID
- Jobs run in a separate goroutine worker

**DB** (`internal/deque/db.go`):
- SQLite database wrapper using GORM
- `Question` model with `SendAt` time and `Content`
- Timezone-aware operations using `db.loc`
- `NextEmptyDate()` finds next available date without questions

### Data Flow

1. User sends text message to bot → Bot parses as questions → Deque schedules to DB and Scheduler
2. Scheduler triggers at scheduled time → Calls job function → Deque retrieves question → Bot sends to chat
3. All questions are loaded from DB on startup and rescheduled

### Question Format Parsing

Questions are parsed line-by-line with optional date/time prefixes (see `internal/deque/deque.go:59-146`):

- `DD.MM HH:MM Question text` - Full date and time
- `DD.MM Question text` - Date with default time
- `HH:MM Question text` - Time with next empty date
- `Question text` - Uses next empty date and default time

Date handling:
- Dates are assumed to be current year unless already passed (then next year)
- "Next empty date" means the next date without any scheduled questions

### Testing Strategy

The test suite (`internal/deque_test/`) uses:
- `MockScheduler` to verify scheduling behavior without real timers
- In-memory SQLite (`:memory:`) for isolated test databases
- Tests verify date/time parsing, scheduling logic, and edge cases

### Important Implementation Details

**Timezone Consistency**:
- The database stores a `*time.Location` passed at initialization
- All time comparisons use `db.Now()` which applies the configured timezone
- Critical for correct date calculations in `NextEmptyDate()`

**Admin Authorization**:
- Bot checks `admin_ids` config for every command/message except target chat
- Target chat (where questions are sent) has no restrictions

**Scheduler Job IDs**:
- Job IDs are question database IDs cast to `JobID` type
- Allows looking up questions when jobs execute
- Jobs are automatically cleaned up after execution

**Message Formatting**:
- Questions can be wrapped with the `format` config string
- Sent with Markdown mode to support formatting
