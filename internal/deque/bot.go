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
	Send(to tele.Recipient, what any, opts ...any) (*tele.Message, error)
	Edit(msg tele.Editable, what any, opts ...any) (*tele.Message, error)
	Delete(msg tele.Editable) error
	Handle(endpoint any, h tele.HandlerFunc, m ...tele.MiddlewareFunc)
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
	bot.api.Handle("/delete", bot.handleDelete)
	bot.api.Handle(tele.OnCallback, bot.handleCallback)

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
	if c.Chat().ID == bot.cfg.ChatID {
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
	_, err := bot.api.Send(&tele.Chat{ID: bot.cfg.ChatID}, text, tele.ModeMarkdown)
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

	_, blocks, err := bot.deque.GetFutureQuestions(15)
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

func (bot *Bot) handleDelete(c tele.Context) error {
	if isAdmin, err := bot.isAdmin(c); !isAdmin {
		return err
	}

	blocks, texts, err := bot.deque.GetFutureQuestions(10)
	if err != nil {
		bot.LogError(err, c)
		_, err = bot.api.Send(c.Chat(), "Произошла ошибка при загрузке списка вопросов.")
		return err
	}
	if len(blocks) == 0 {
		_, err := bot.api.Send(c.Chat(), texts[0])
		return err
	}

	// If there's only one question, skip the list and show confirmation immediately
	if len(blocks) == 1 && len(blocks[0]) == 1 {
		question := blocks[0][0]
		return bot.sendDeleteConfirmation(c, question, false)
	}

	// Send the first block immediately
	if len(blocks) > 0 {
		err := bot.sendDeleteBlock(c, blocks[0], texts[0])
		if err != nil {
			return err
		}
	}

	// Send remaining blocks with delay
	for i := 1; i < len(blocks); i++ {
		time.Sleep(500 * time.Millisecond)
		err := bot.sendDeleteBlock(c, blocks[i], texts[i])
		if err != nil {
			return err
		}
	}

	return nil
}

func (bot *Bot) sendDeleteBlock(c tele.Context, questions []Question, text string) error {
	var buttons [][]tele.InlineButton
	for _, q := range questions {
		btn := tele.InlineButton{
			Text: fmt.Sprintf("Удалить вопрос %s", q.SendAt.Format("02.01")),
			Data: fmt.Sprintf("delete|%d", q.ID),
		}
		buttons = append(buttons, []tele.InlineButton{btn})
	}

	markup := &tele.ReplyMarkup{
		InlineKeyboard: buttons,
	}

	_, err := bot.api.Send(c.Chat(), text, markup)
	return err
}

func (bot *Bot) handleCallback(c tele.Context) error {
	callback := c.Callback()
	if callback == nil {
		return nil
	}

	parts := strings.Split(callback.Data, "|")
	action := parts[0]

	switch action {
	case "delete":
		if len(parts) < 2 {
			return nil
		}
		return bot.handleDeleteCallback(c, parts[1])
	case "confirm":
		if len(parts) < 2 {
			return nil
		}
		return bot.handleConfirmCallback(c, parts[1])
	case "cancel":
		return bot.handleCancelCallback(c)
	}

	return nil
}

func (bot *Bot) sendDeleteConfirmation(c tele.Context, question Question, deleteListMessage bool) error {
	if deleteListMessage {
		err := bot.api.Delete(c.Message())
		if err != nil {
			bot.LogError(err, c)
		}
	}

	confirmText := fmt.Sprintf("Удалить вопрос?\n\n%s\n%s",
		question.SendAt.Format("02.01.2006 15:04"),
		question.Content)

	yesBtn := tele.InlineButton{
		Text: "Да",
		Data: fmt.Sprintf("confirm|%d", question.ID),
	}
	noBtn := tele.InlineButton{
		Text: "Нет",
		Data: "cancel",
	}

	markup := &tele.ReplyMarkup{
		InlineKeyboard: [][]tele.InlineButton{{yesBtn, noBtn}},
	}

	_, err := bot.api.Send(c.Chat(), confirmText, markup)
	return err
}

func (bot *Bot) handleDeleteCallback(c tele.Context, questionIDStr string) error {
	var questionID uint
	_, err := fmt.Sscanf(questionIDStr, "%d", &questionID)
	if err != nil {
		bot.LogError(err, c)
		return c.Respond(&tele.CallbackResponse{
			Text: "Ошибка при обработке запроса.",
		})
	}

	question, err := bot.db.GetQuestionByID(questionID)
	if err != nil {
		bot.LogError(err, c)
		return c.Respond(&tele.CallbackResponse{
			Text: "Вопрос не найден.",
		})
	}

	return bot.sendDeleteConfirmation(c, question, true)
}

func (bot *Bot) handleConfirmCallback(c tele.Context, questionIDStr string) error {
	var questionID uint
	_, err := fmt.Sscanf(questionIDStr, "%d", &questionID)
	if err != nil {
		bot.LogError(err, c)
		return c.Respond(&tele.CallbackResponse{
			Text: "Ошибка при обработке запроса.",
		})
	}

	err = bot.deque.DeleteQuestion(questionID)
	if err != nil {
		bot.LogError(err, c)
		updatedText := c.Message().Text + "\n\n❌ Ошибка при удалении вопроса."
		_, editErr := bot.api.Edit(c.Message(), updatedText)
		return editErr
	}

	updatedText := c.Message().Text + "\n\n✅ Вопрос успешно удален."
	_, err = bot.api.Edit(c.Message(), updatedText)
	return err
}

func (bot *Bot) handleCancelCallback(c tele.Context) error {
	updatedText := c.Message().Text + "\n\n❌ Удаление отменено."
	_, err := bot.api.Edit(c.Message(), updatedText)
	return err
}
