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
	bot.api.Handle("/list", bot.handleList)
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

func (bot *Bot) isAdmin(c tele.Context) (bool, error) {
	senderID := c.Sender().ID
	isAdmin := slices.Contains(bot.cfg.AdminIDs, senderID)
	if !isAdmin {
		_, err := bot.api.Send(c.Chat(), "Извините, у вас нет прав для использования этого бота.")
		return false, err
	}

	return true, nil
}

func (bot *Bot) handleText(c tele.Context) error {
	if !c.Chat().Private {
		return nil
	}

	if isAdmin, err := bot.isAdmin(c); !isAdmin {
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

func (bot *Bot) AskQuestion(text string) {
	_, err := bot.api.Send(&tele.Chat{ID: bot.cfg.ChatID}, text)
	if err != nil {
		bot.LogError(err, nil)
	}
}

func (bot *Bot) handleHelp(c tele.Context) error {
	if isAdmin, err := bot.isAdmin(c); !isAdmin {
		return err
	}

	helpText := bot.deque.GetHelp()
	_, err := bot.api.Send(c.Chat(), helpText)
	return err
}

func (bot *Bot) handleList(c tele.Context) error {
	if isAdmin, err := bot.isAdmin(c); !isAdmin {
		return err
	}

	blocks, err := bot.deque.GetFutureQuestions()
	if err != nil {
		bot.LogError(err, c)
		_, err = bot.api.Send(c.Chat(), "Произошла ошибка при загрузке списка вопросов.")
		return err
	}

	// Send the first block immediately
	if len(blocks) > 0 {
		_, err = bot.api.Send(c.Chat(), blocks[0])
		if err != nil {
			bot.LogError(err, c)
			return err
		}
	}

	// Send remaining blocks with delay
	for i := 1; i < len(blocks); i++ {
		time.Sleep(500 * time.Millisecond) // Wait 500ms between messages
		_, err = bot.api.Send(c.Chat(), blocks[i])
		if err != nil {
			bot.LogError(err, c)
			return err
		}
	}

	return nil
}

func (bot *Bot) handleStats(c tele.Context) error {
	if isAdmin, err := bot.isAdmin(c); !isAdmin {
		return err
	}

	statsText, err := bot.deque.GetStats()
	if err != nil {
		bot.LogError(err, c)
		_, err = bot.api.Send(c.Chat(), "Произошла ошибка при загрузке статистики.")
		return err
	}

	_, err = bot.api.Send(c.Chat(), statsText)
	return err
}
