package deque

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
	"gopkg.in/telebot.v3/middleware"
)

type Bot struct {
	api   BotAPI
	db    *DB
	log   *zap.SugaredLogger
	cfg   Config
	deque *Deque
}

type BotAPI interface {
	Send(to tele.Recipient, what interface{}, opts ...interface{}) (*tele.Message, error)
	Handle(endpoint interface{}, h tele.HandlerFunc, m ...tele.MiddlewareFunc)
	Use(middlewares ...tele.MiddlewareFunc)
	Start()
	Stop()
}

type JobID uint
type JobFunc func(JobID)

type Scheduler interface {
	SetJobFunc(fn JobFunc)
	Schedule(at time.Time, id JobID)
	Cancel(id JobID)
}

func NewBot(db *DB) *Bot {
	bot := &Bot{
		db:  db,
		log: zap.L().Named("bot").Sugar(),
	}

	return bot
}

func (bot *Bot) Logger() *zap.SugaredLogger {
	return bot.log
}

func (bot *Bot) Start(cfg Config, api BotAPI, deque *Deque) {
	bot.api = api
	bot.cfg = cfg
	bot.deque = deque

	bot.api.Use(middleware.Recover())
	bot.api.Use(bot.logMessage)

	bot.api.Handle(tele.OnText, bot.handleText)
	bot.api.Handle("/start", bot.handleHelp)
	bot.api.Handle("/help", bot.handleHelp)
	bot.api.Handle("/stat", bot.handleStats)

	// Set up the ask function
	bot.deque.SetAskFunc(bot.AskQuestion)

	go func() {
		bot.log.Info("starting bot")
		bot.api.Start()
		bot.log.Info("bot stopped")
	}()
}

func (bot *Bot) Stop() {
	bot.api.Stop()
}

func (bot *Bot) logMessage(next tele.HandlerFunc) tele.HandlerFunc {
	return func(c tele.Context) error {
		beginTime := time.Now().UnixNano()

		err := next(c)

		endTime := time.Now().UnixNano()
		duration := float64(endTime-beginTime) / 1000000

		isCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1
		isAction := c.Callback() != nil
		var action string
		if isCmd {
			action = c.Text()
		} else if isAction {
			action = strings.TrimSpace(strings.Split(c.Callback().Data, "|")[0])
		}
		bot.log.Infow("user message",
			"chat_id", c.Chat().ID,
			"chat_type", c.Chat().Type,
			"user_id", c.Sender().ID,
			"user_name", c.Sender().Username,
			"is_cmd", isCmd,
			"is_action", isAction,
			"action", action,
			"size", len(c.Text()),
			"dur", fmt.Sprintf("%.2f", duration),
			"err", err)

		return err
	}
}

func (bot *Bot) LogError(err error, c tele.Context) {
	if c == nil {
		bot.log.Errorw("error", "err", err)
	} else {
		isCmd := len(c.Text()) > 0 && c.Text()[0] == '/' && len(c.Entities()) == 1
		isAction := c.Callback() != nil
		var action string
		if isCmd {
			action = c.Text()
			idx := strings.Index(action, " ")
			if idx > 0 {
				action = action[:idx]
			}
		} else if isAction {
			action = strings.TrimSpace(strings.Split(c.Callback().Data, "|")[0])
		}
		bot.log.Errorw("error",
			"chat_id", c.Chat().ID,
			"chat_type", c.Chat().Type,
			"user_id", c.Sender().ID,
			"user_name", c.Sender().Username,
			"is_cmd", isCmd,
			"is_action", isAction,
			"action", action,
			"size", len(c.Text()),
			"err", err)
	}
}

func (bot *Bot) handleText(c tele.Context) error {
	// Check if sender is admin
	senderID := c.Sender().ID
	isAdmin := slices.Contains(bot.cfg.AdminIDs, senderID)
	if !isAdmin {
		_, err := bot.api.Send(c.Chat(), "Извините, у вас нет прав для использования этого бота.")
		return err
	}

	// Schedule questions from the message text
	err := bot.deque.ScheduleQuestions(c.Text())
	if err != nil {
		bot.LogError(err, c)
		_, err = bot.api.Send(c.Chat(), "Произошла ошибка при планировании вопросов. Пожалуйста, проверьте формат сообщения.")
		return err
	}

	_, err = bot.api.Send(c.Chat(), "Вопросы успешно запланированы.")
	return err
}

func (bot *Bot) AskQuestion(q Question) {
	text := bot.cfg.CommonText
	if text != "" {
		text += "\n\n"
	}
	text += q.Content

	_, err := bot.api.Send(&tele.Chat{ID: bot.cfg.ChatID}, text)
	if err != nil {
		bot.LogError(err, nil)
	}
}

func (bot *Bot) handleHelp(c tele.Context) error {
	// Check if sender is admin
	senderID := c.Sender().ID
	isAdmin := slices.Contains(bot.cfg.AdminIDs, senderID)
	if !isAdmin {
		_, err := bot.api.Send(c.Chat(), "Извините, у вас нет прав для использования этого бота.")
		return err
	}

	helpText := `Инструкция по планированию вопросов:

1. Каждый вопрос должен быть на отдельной строке
2. Формат строки: [ДД.ММ] [ЧЧ:ММ] текст вопроса
   - Дата и время опциональны
   - Если дата не указана, вопрос будет запланирован на следующую свободную дату
   - Если время не указано, будет использовано время по умолчанию

Примеры:
25.12 15:30 Какой подарок вы хотите на Новый год?
10:00 Как у вас дела сегодня?
Что вы думаете о погоде?`

	_, err := bot.api.Send(c.Chat(), helpText)
	return err
}

func (bot *Bot) handleStats(c tele.Context) error {
	// Check if sender is admin
	senderID := c.Sender().ID
	isAdmin := slices.Contains(bot.cfg.AdminIDs, senderID)
	if !isAdmin {
		_, err := bot.api.Send(c.Chat(), "Извините, у вас нет прав для использования этого бота.")
		return err
	}

	stats, err := bot.db.LoadStats()
	if err != nil {
		bot.LogError(err, c)
		_, err = bot.api.Send(c.Chat(), "Произошла ошибка при загрузке статистики.")
		return err
	}

	statsText := fmt.Sprintf(`📊 Статистика вопросов:

Всего вопросов: %d
├ Прошедших: %d
└ Предстоящих: %d`,
		stats.TotalQuestions,
		stats.PastQuestions,
		stats.FutureQuestions)

	if stats.FutureQuestions > 0 {
		statsText += fmt.Sprintf(`

Следующий вопрос:
📅 %s
❔ %s`,
			stats.UpcomingQuestion.SendAt.Format("02.01.2006 15:04"),
			stats.UpcomingQuestion.Content)
	} else {
		statsText += "\n\nНет запланированных вопросов."
	}

	_, err = bot.api.Send(c.Chat(), statsText)
	return err
}
