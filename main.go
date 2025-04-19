package main

import (
	"deque/internal/deque"
	"os"
	"os/signal"
	"time"

	"go.uber.org/zap"
	tele "gopkg.in/telebot.v3"
)

func main() {
	cfg, err := deque.LoadConfig()
	if err != nil {
		panic(err)
	}

	var zapLogger *zap.Logger
	if cfg.Release {
		zapLogger, err = zap.NewProduction(zap.WithCaller(false))
	} else {
		zapLogger, err = zap.NewDevelopment(zap.WithCaller(false))
	}
	if err != nil {
		panic(err)
	}
	defer func() { _ = zapLogger.Sync() }()

	zap.ReplaceGlobals(zapLogger)
	zap.RedirectStdLog(zapLogger)
	logger := zapLogger.Sugar()

	location, err := time.LoadLocation(cfg.Location)
	if err != nil {
		logger.Panicf("invalid location: %w", err)
	}

	db, ok := deque.LoadDatabase(cfg.DBPath, location)
	if !ok {
		logger.Panic("can't load database")
	}

	msgSched := deque.NewSched(cfg.MaxJobs)
	msgSched.Start()
	defer msgSched.Stop()

	deq := deque.NewDeque(db, cfg, msgSched)
	bot := deque.NewBot(db)

	pref := tele.Settings{
		Token:   cfg.TgToken,
		Poller:  &tele.LongPoller{Timeout: 30 * time.Second},
		OnError: bot.LogError,
	}

	api, err := tele.NewBot(pref)
	if err != nil {
		logger.Panic(err)
	}

	bot.Start(cfg, api, deq)
	defer bot.Stop()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
}
