package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"report_redmine/internal/adapter/calendar"
	"report_redmine/internal/adapter/export/excel"
	"report_redmine/internal/adapter/storage/postgres"
	"report_redmine/internal/config"
	service2 "report_redmine/internal/service"
	"strings"
	"syscall"
	"time"

	"github.com/urfave/cli/v3"
)

const layout = "02.01.2006"

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	cmd := &cli.Command{
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "log",
				Value:   "info",
				Usage:   "Set log level (debug, info, warn, error)",
				Aliases: []string{"level"},
			},
			&cli.StringFlag{
				Name:    "db-host",
				Usage:   "Database host",
				Aliases: []string{"host"},
			},
			&cli.IntFlag{
				Name:    "db-port",
				Value:   5432,
				Usage:   "Database port",
				Aliases: []string{"port"},
			},
			&cli.StringFlag{
				Name:    "db-user",
				Usage:   "Database user",
				Aliases: []string{"user"},
			},
			&cli.StringFlag{
				Name:    "db-password",
				Usage:   "Database password",
				Aliases: []string{"pass"},
			},
			&cli.StringFlag{
				Name:    "db-name",
				Usage:   "Database name",
				Aliases: []string{"name"},
			},
			&cli.StringFlag{
				Name:    "issues-path",
				Value:   "",
				Usage:   "Path to issues file",
				Aliases: []string{"path"},
			},
			&cli.StringFlag{
				Name:     "start-date",
				Usage:    "Start date in format DD.MM.YYYY",
				Aliases:  []string{"from"},
				Required: true,
			},
			&cli.StringFlag{
				Name:     "end-date",
				Usage:    "End date in format DD.MM.YYYY",
				Aliases:  []string{"to"},
				Required: true,
			},
			&cli.IntFlag{
				Name:    "project",
				Usage:   "Project ID to Redmine",
				Value:   25,
				Aliases: []string{"p"},
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {

			cfg := config.MustGetConfig()

			cfg.LogLevel = cmd.String("log")

			if host := cmd.String("db-host"); host != "" {
				cfg.Database.Host = host
			}
			if port := cmd.Int("db-port"); port != 0 {
				cfg.Database.Port = port
			}
			if user := cmd.String("db-user"); user != "" {
				cfg.Database.User = user
			}
			if password := cmd.String("db-password"); password != "" {
				cfg.Database.Password = password
			}
			if dbname := cmd.String("db-name"); dbname != "" {
				cfg.Database.DBName = dbname
			}
			if issuesPatch := cmd.String("issues-path"); issuesPatch != "" {
				cfg.Redmine.IssuePatch = issuesPatch
			}
			if projectID := cmd.Int("project"); projectID != 0 {
				cfg.Redmine.ProjectID = projectID
			}

			startDateStr := cmd.String("start-date")
			endDateStr := cmd.String("end-date")

			var err error

			cfg.Redmine.StartDate, err = time.Parse(layout, startDateStr)
			if err != nil {
				return fmt.Errorf("invalid start-date format '%s', expected DD.MM.YYYY", startDateStr)
			}

			cfg.Redmine.EndDate, err = time.Parse(layout, endDateStr)
			if err != nil {
				return fmt.Errorf("invalid end-date format '%s', expected DD.MM.YYYY", endDateStr)
			}

			if cfg.Redmine.StartDate.After(cfg.Redmine.EndDate) {
				return fmt.Errorf("start-date cannot be after end-date")
			}

			return nil
		},
	}

	if err := cmd.Run(ctx, os.Args); err != nil {
		log.Fatal(err)
	}

	cfg := config.MustGetConfig()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: parseLogLevel(cfg.LogLevel),
		//AddSource: true,
	}))
	slog.SetDefault(logger)

	pgPool, err := postgres.InitStorage(ctx, cfg)
	if err != nil {
		logger.Error("Failed to initialize PostgresSQL storage", slog.Any("error", err))
		panic(err)
	}

	pgStorage := postgres.NewStorage(pgPool, cfg)

	cal := calendar.New()

	export := excel.New()

	service := service2.NewService(pgStorage, cal, cfg, export)

	if err = service.NewReport(ctx); err != nil {
		logger.Error("Failed to create report", slog.Any("error", err))
	}

	/*<-done
	logger.Info("Gracefully shutting down")

	// Отменяем контекст
	cancel()
	logger.Info("stopping server")*/
}

func parseLogLevel(levelStr string) slog.Level {
	switch strings.ToLower(levelStr) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
