package service

import (
	"context"
	"fmt"
	"log/slog"
	"report_redmine/internal/adapter/calendar"
	"report_redmine/internal/adapter/export/excel"
	"report_redmine/internal/config"
	"report_redmine/internal/entities"
	"time"
)

type Service struct {
	storage  Storage
	cfg      *config.Config
	calendar *calendar.Calendar
	excel    *excel.Exporter
}

func NewService(storage Storage, calendar *calendar.Calendar, cfg *config.Config, excel *excel.Exporter) *Service {
	return &Service{
		storage:  storage,
		calendar: calendar,
		cfg:      cfg,
		excel:    excel,
	}
}

func (s *Service) NewReport(ctx context.Context) error {
	const op = "storage.NewReport"

	// Устанавливаем PeriodStart и PeriodEnd из конфига с учётом начала и конца дня
	PeriodStart := time.Date(s.cfg.StartDate.Year(), s.cfg.StartDate.Month(), s.cfg.StartDate.Day(), 0, 0, 0, 0, s.cfg.StartDate.Location())
	PeriodEnd := time.Date(s.cfg.EndDate.Year(), s.cfg.EndDate.Month(), s.cfg.EndDate.Day(), 23, 59, 59, 0, s.cfg.EndDate.Location())

	slog.Info("Generating report",
		slog.Time("from", PeriodStart),
		slog.Time("to", PeriodEnd))

	req := entities.IssueRequest{
		ProjectID:          25,
		PeriodStart:        PeriodStart,
		PeriodEnd:          PeriodEnd,
		IncludeHistory:     true,
		IncludeTimeEntries: true,
	}

	issues, err := s.storage.GetRawIssues(ctx, req)
	if err != nil {
		slog.Error("Failed to get issues", slog.Any("error", err), "op", op)
		return err
	}

	minDate := req.PeriodStart
	maxDate := req.PeriodEnd

	for _, issue := range issues {
		if issue.CreateDate.Before(minDate) {
			minDate = issue.CreateDate
		}
		if !issue.ResolvedDate.IsZero() && issue.ResolvedDate.After(maxDate) {
			maxDate = issue.ResolvedDate
		}
	}

	minDate = minDate.AddDate(0, -1, 0)
	maxDate = maxDate.AddDate(0, 1, 0)

	err = s.calendar.LoadPeriod(minDate, maxDate)
	if err != nil {
		slog.Warn("Failed to preload full calendar, will load on-demand", "error", err)
	}

	if err = s.calcSLA(issues); err != nil {
		slog.Error("Failed to calculate SLA", slog.Any("error", err), "op", op)
	}

	if err = s.GetDeadlines(issues); err != nil {
		slog.Error("Failed to get deadlines", slog.Any("error", err), "op", op)
	}

	select {
	case <-ctx.Done():
		slog.Info("Context cancelled")
		return ctx.Err()
	default:
	}

	// Экспорт в Excel
	outputFile := fmt.Sprintf("report_%s.xlsx", time.Now().Format("20060102_150405"))
	if err = s.excel.Export(issues, outputFile); err != nil {
		slog.Error("Failed to export to Excel", slog.Any("error", err), "op", op)
		return err
	}

	slog.Info("Report generated successfully", "file", outputFile)

	return nil
}

func (s *Service) calcSLA(issues []entities.Issue) error {
	for i := 0; i < len(issues); i++ {
		if len(issues[i].StatusHistory) == 0 {
			continue
		}

		for j := 0; j < len(issues[i].StatusHistory); j++ {
			if issues[i].StatusHistory[j].PropertyKey == "status_id" && issues[i].StatusHistory[j].NewValueName == "Решена" {
				issues[i].ResolvedDate = issues[i].StatusHistory[j].ChangeDate
			}

			if issues[i].StatusHistory[j].PropertyKey == "status_id" {

				switch issues[i].StatusHistory[j].OldValueName {
				case "Новая", "В работе":
					if issues[i].Priority == "Нулевой приоритет" || (issues[i].Priority == "Первый приоритет" && *issues[i].SubprojectSBS == "ССО") {
						if j == 0 {
							issues[i].SLA = s.calculateHoursHigh(issues[i].CreateDate, issues[i].StatusHistory[j].ChangeDate)
						} else {
							issues[i].SLA = issues[i].SLA + s.calculateHoursHigh(issues[i].StatusHistory[j-1].ChangeDate, issues[i].StatusHistory[j].ChangeDate)
						}
					} else {
						if j == 0 {
							issues[i].SLA = s.calculateHoursBetween(issues[i].CreateDate, issues[i].StatusHistory[j].ChangeDate)
						} else {
							issues[i].SLA = issues[i].SLA + s.calculateHoursBetween(issues[i].StatusHistory[j-1].ChangeDate, issues[i].StatusHistory[j].ChangeDate)
						}
					}

				case "Решена":
					continue
				case "Обратная связь":
					continue
				}
			}

			if issues[i].StatusHistory[j].PropertyKey == "priority_id" {
				issues[i].SLA = 0.0
			}
		}
	}

	return nil
}

func (s *Service) calculateHoursBetween(start, end time.Time) float64 {
	if start.After(end) {
		return 0
	}

	var totalHours float64
	current := start.Truncate(24 * time.Hour) // Начало дня старта
	endDay := end.Truncate(24 * time.Hour)    // Конец дня окончания

	// Рабочее время: с 9:00 до 18:00
	workStart := 9 * time.Hour
	workEnd := 18 * time.Hour

	for !current.After(endDay) {
		loc := start.Location()
		dayStart := time.Date(current.Year(), current.Month(), current.Day(), 0, 0, 0, 0, loc)
		workStartToday := dayStart.Add(workStart)
		workEndToday := dayStart.Add(workEnd)

		// Проверяем, является ли день рабочим
		isWorkDay, err := s.calendar.IsWorkDay(current)
		if err != nil {
			// В случае ошибки считаем день рабочим
			isWorkDay = true
		}
		if !isWorkDay {
			current = current.AddDate(0, 0, 1)
			continue
		}

		// Определяем фактическое начало и конец учётного времени в этот день
		dayWorkStart := workStartToday
		if start.After(dayWorkStart) {
			dayWorkStart = start
		}
		// Если начало задачи уже после 18:00 — пропускаем
		if dayWorkStart.After(workEndToday) {
			current = current.AddDate(0, 0, 1)
			continue
		}

		dayWorkEnd := workEndToday
		if end.Before(dayWorkEnd) {
			dayWorkEnd = end
		}
		// Если конец задачи до 9:00 — пропускаем
		if dayWorkEnd.Before(workStartToday) {
			current = current.AddDate(0, 0, 1)
			continue
		}

		// Добавляем только пересечение с рабочим временем
		if dayWorkEnd.After(dayWorkStart) {
			duration := dayWorkEnd.Sub(dayWorkStart)
			totalHours += duration.Hours()
		}

		// Переход к следующему дню
		current = current.AddDate(0, 0, 1)
	}

	return totalHours
}

// Простая функция для расчета часов между двумя датами
func (s *Service) calculateHoursHigh(start, end time.Time) float64 {

	if start.After(end) {
		return 0
	}

	duration := end.Sub(start)

	hours := duration.Hours()

	return hours
}

func (s *Service) GetDeadlines(issues []entities.Issue) error {
	if len(issues) == 0 {
		return nil
	}

	for i := 0; i < len(issues); i++ {
		if issues[i].SubprojectSBS == nil {
			continue
		}

		switch issues[i].Priority {
		case "Нулевой приоритет":
			switch *issues[i].SubprojectSBS {
			case "Авто":
				issues[i].DeadlineSLA = 3
			case "ССО", "ЛКФЗл", "ЛКЮРл":
				issues[i].DeadlineSLA = 2
			default:
				continue
			}
		case "Первый приоритет":
			switch *issues[i].SubprojectSBS {
			case "Авто":
				issues[i].DeadlineSLA = 8
			case "ССО", "ЛКФЗл", "ЛКЮРл":
				issues[i].DeadlineSLA = 4
			default:
				continue
			}
		case "Второй приоритет":
			switch *issues[i].SubprojectSBS {
			case "Авто":
				issues[i].DeadlineSLA = 16
			case "ССО", "ЛКФЗл", "ЛКЮРл":
				issues[i].DeadlineSLA = 16
			default:
				continue
			}
		case "Третий приоритет":
			switch *issues[i].SubprojectSBS {
			case "Авто":
				issues[i].DeadlineSLA = 24
			case "ССО", "ЛКФЗл", "ЛКЮРл":
				issues[i].DeadlineSLA = 24
			default:
				continue
			}
		default:
			continue
		}

		issues[i].MissingSLA = issues[i].SLA - issues[i].DeadlineSLA
	}

	return nil
}
