package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

	start := time.Now()

	// Устанавливаем PeriodStart и PeriodEnd из конфига с учётом начала и конца дня
	PeriodStart := time.Date(s.cfg.Redmine.StartDate.Year(), s.cfg.Redmine.StartDate.Month(), s.cfg.Redmine.StartDate.Day(), 0, 0, 0, 0, s.cfg.Redmine.StartDate.Location())
	PeriodEnd := time.Date(s.cfg.Redmine.EndDate.Year(), s.cfg.Redmine.EndDate.Month(), s.cfg.Redmine.EndDate.Day(), 23, 59, 59, 0, s.cfg.Redmine.EndDate.Location())

	req := entities.IssueRequest{
		ProjectID:          s.cfg.Redmine.ProjectID,
		PeriodStart:        PeriodStart,
		PeriodEnd:          PeriodEnd,
		IncludeHistory:     s.cfg.Redmine.IncludeHistory,
		IncludeTimeEntries: s.cfg.Redmine.IncludeTimeEntries,
		ProjectType:        s.cfg.Redmine.TypeProject,
	}

	slog.Info("Generating report",
		slog.Time("from", PeriodStart),
		slog.Time("to", PeriodEnd),
		slog.Any("project-id", s.cfg.Redmine.ProjectID),
	)

	startTime := time.Now()

	issues, err := s.storage.GetRawIssues(ctx, req)
	if err != nil {
		slog.Error("Failed to get issues", slog.Any("error", err), "op", op)
		return err
	}

	if len(issues) == 0 {
		slog.Info("No issues found", slog.Any("issues", req))
		return nil
	}

	issues[0].ProjectType = s.cfg.Redmine.TypeProject

	dbTime := time.Since(startTime)
	slog.Debug("Raw issues generated in ", "time", dbTime.String())

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

	startTime = time.Now()

	err = s.calendar.LoadPeriod(minDate, maxDate)
	if err != nil {
		slog.Warn("Failed to preload full calendar, will load on-demand", "error", err)
	}

	calcCalendarDuration := time.Since(startTime)
	slog.Debug("Calendar load generated in ", "time", calcCalendarDuration.String())

	startTime = time.Now()
	if err = s.calculateSLA(issues); err != nil {
		slog.Error("Failed to calculate SLA", slog.Any("error", err), "op", op)
	}
	callCalcDuration := time.Since(startTime)
	slog.Debug("Calc SLA generated in ", "time", callCalcDuration.String())

	startTime = time.Now()

	if err = s.GetDeadlines(issues); err != nil {
		slog.Error("Failed to get deadlines", slog.Any("error", err), "op", op)
	}
	deadlineDuration := time.Since(startTime)
	slog.Debug("Deadline generated in ", "time", deadlineDuration.String())

	startTime = time.Now()
	if err = s.GetFields(issues); err != nil {
		slog.Error("Failed to get fields", slog.Any("error", err), "op", op)
	}
	getFieldsDuration := time.Since(startTime)
	slog.Debug("Get Fields duration", "seconds", getFieldsDuration.String())

	select {
	case <-ctx.Done():
		slog.Info("Context cancelled")
		return ctx.Err()
	default:
	}

	// Определяем путь для сохранения файла
	outputDir := s.cfg.Redmine.IssuePatch
	if outputDir == "" {
		// Если путь не задан — используем текущую директорию
		outputDir = "."
	}

	// Обеспечиваем кроссплатформенную совместимость пути
	outputDir = filepath.Clean(outputDir)

	// Создаём имя файла
	filename := fmt.Sprintf("report_%s.xlsx", time.Now().Format("20060102_150405"))

	// Формируем полный путь
	outputFile := filepath.Join(outputDir, filename)

	// Проверяем, существует ли директория, и создаём при необходимости
	if err = os.MkdirAll(outputDir, 0755); err != nil {
		slog.Error("Failed to create output directory", slog.Any("error", err), slog.String("dir", outputDir))
		return fmt.Errorf("%s: failed to create output directory: %w", op, err)
	}

	if err = s.excel.Export(issues, outputFile); err != nil {
		slog.Error("Failed to export to Excel", slog.Any("error", err), "op", op)
		return err
	}

	slog.Info("Report generated successfully",
		slog.String("file", outputFile),
		slog.String("abs", getAbsPath(outputFile)),
		slog.String("duration", time.Since(start).String()),
	)

	return nil
}

func (s *Service) calculateSLA(issues []entities.Issue) error {
	if len(issues) == 0 {
		return nil
	}

	switch issues[0].ProjectType {
	case config.ProjectTypeSBS:
		return s.calculateSLASBS(issues)
	default:
		return s.calculateSLADefault(issues)
	}
}

func (s *Service) calculateSLASBS(issues []entities.Issue) error {
	for i := range issues {
		if len(issues[i].StatusHistory) == 0 {
			continue
		}

		for j := range issues[i].StatusHistory {
			if issues[i].StatusHistory[j].PropertyKey == "status_id" && issues[i].StatusHistory[j].NewValueName == "Решена" {
				issues[i].ResolvedDate = issues[i].StatusHistory[j].ChangeDate
			}

			if issues[i].StatusHistory[j].PropertyKey == "status_id" {

				switch issues[i].StatusHistory[j].OldValueName {
				case "Новая", "В работе":
					if issues[i].Priority == "Нулевой приоритет" || (issues[i].Priority == "Первый приоритет" && issues[i].SubprojectSBS == "ССО") {
						if j == 0 {
							issues[i].SLA = s.calculateCalendarHours(issues[i].CreateDate, issues[i].StatusHistory[j].ChangeDate)
						} else {
							issues[i].SLA = issues[i].SLA + s.calculateCalendarHours(issues[i].StatusHistory[j-1].ChangeDate, issues[i].StatusHistory[j].ChangeDate)
						}
					} else {
						if j == 0 {
							issues[i].SLA = s.calculateWorkingHours(issues[i].CreateDate, issues[i].StatusHistory[j].ChangeDate)
						} else {
							issues[i].SLA = issues[i].SLA + s.calculateWorkingHours(issues[i].StatusHistory[j-1].ChangeDate, issues[i].StatusHistory[j].ChangeDate)
						}
					}

				case "Решена":
					continue
				case "Обратная связь":
					continue
				default:
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

func (s *Service) calculateSLADefault(issues []entities.Issue) error {
	for i := range issues {
		if len(issues[i].StatusHistory) == 0 {
			continue
		}

		for j := range issues[i].StatusHistory {
			if issues[i].StatusHistory[j].PropertyKey == "status_id" && issues[i].StatusHistory[j].NewValueName == "Решена" {
				issues[i].ResolvedDate = issues[i].StatusHistory[j].ChangeDate
			}

			if issues[i].StatusHistory[j].PropertyKey == "status_id" {

				switch issues[i].StatusHistory[j].OldValueName {
				case "Новая", "В работе":
					if j == 0 {
						issues[i].SLA = s.calculateWorkingHours(issues[i].CreateDate, issues[i].StatusHistory[j].ChangeDate)
					} else {
						issues[i].SLA = issues[i].SLA + s.calculateWorkingHours(issues[i].StatusHistory[j-1].ChangeDate, issues[i].StatusHistory[j].ChangeDate)
					}
				case "Решена":
					continue
				case "Обратная связь":
					continue
				default:
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

// Функция для расчета SLA по рабочим дням
func (s *Service) calculateWorkingHours(start, end time.Time) float64 {
	if start.After(end) {
		return 0
	}

	var totalHours float64
	current := start.Truncate(24 * time.Hour)
	endDay := end.Truncate(24 * time.Hour)

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

// Функция для расчета SLA по всем дням 24\7\365
func (s *Service) calculateCalendarHours(start, end time.Time) float64 {

	if start.After(end) {
		return 0
	}

	duration := end.Sub(start)

	hours := duration.Hours()

	return hours
}

func (s *Service) GetDeadlines(issues []entities.Issue) error {
	switch issues[0].ProjectType {
	case config.ProjectTypeSBS:
		return s.GetDeadlinesSBS(issues)
	case config.ProjectTypeIngos:
		return s.GetDeadlinesIngos(issues)
	case config.ProjectTypeSoglasie:
		return s.GetDeadlinesSoglasie(issues)
	case config.ProjectTypeZetta:
		return s.GetDeadlinesZetta(issues)
	}

	return nil
}

func (s *Service) GetDeadlinesSBS(issues []entities.Issue) error {
	for i := range issues {
		switch issues[i].Priority {
		case "Нулевой приоритет":
			switch issues[i].SubprojectSBS {
			case "Авто":
				issues[i].DeadlineSLA = 3
			case "ССО", "ЛКФЗл", "ЛКЮРл":
				issues[i].DeadlineSLA = 2
			default:
				continue
			}
		case "Первый приоритет":
			switch issues[i].SubprojectSBS {
			case "Авто":
				issues[i].DeadlineSLA = 8
			case "ССО", "ЛКФЗл", "ЛКЮРл":
				issues[i].DeadlineSLA = 4
			default:
				continue
			}
		case "Второй приоритет":
			switch issues[i].SubprojectSBS {
			case "Авто":
				issues[i].DeadlineSLA = 16
			case "ССО", "ЛКФЗл", "ЛКЮРл":
				issues[i].DeadlineSLA = 16
			default:
				continue
			}
		case "Третий приоритет":
			switch issues[i].SubprojectSBS {
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
func (s *Service) GetDeadlinesIngos(issues []entities.Issue) error {
	for i := range issues {
		switch issues[i].Priority {
		case "Нулевой приоритет":
			issues[i].DeadlineSLA = 28
		case "Первый приоритет":
			issues[i].DeadlineSLA = 28
		case "Второй приоритет":
			issues[i].DeadlineSLA = 34
		case "Третий приоритет":
			issues[i].DeadlineSLA = 34
		default:
			continue
		}

		issues[i].MissingSLA = issues[i].SLA - issues[i].DeadlineSLA
	}

	return nil
}
func (s *Service) GetDeadlinesSoglasie(issues []entities.Issue) error {
	for i := range issues {
		switch issues[i].Priority {
		case "Нулевой приоритет":
			issues[i].DeadlineSLA = 16
		case "Первый приоритет":
			issues[i].DeadlineSLA = 32
		case "Второй приоритет":
			issues[i].DeadlineSLA = 96
		case "Третий приоритет":
			issues[i].DeadlineSLA = 184
		default:
			continue
		}

		issues[i].MissingSLA = issues[i].SLA - issues[i].DeadlineSLA
	}

	return nil
}

func (s *Service) GetDeadlinesZetta(issues []entities.Issue) error {
	for i := range issues {
		switch issues[i].Priority {
		case "Нулевой приоритет":
			issues[i].DeadlineSLA = 16
		case "Первый приоритет":
			issues[i].DeadlineSLA = 32
		case "Второй приоритет":
			issues[i].DeadlineSLA = 96
		case "Третий приоритет":
			issues[i].DeadlineSLA = 184
		default:
			continue
		}

		issues[i].MissingSLA = issues[i].SLA - issues[i].DeadlineSLA
	}

	return nil
}

func (s *Service) GetFields(issues []entities.Issue) error {
	for i := range issues {
		for j := range issues[i].StatusHistory {
			if issues[i].StatusHistory[j].Notes != "" {
				issues[i].LastComment = issues[i].StatusHistory[j].Notes
				issues[i].LastCommentator = issues[i].StatusHistory[j].UserLastname + " " + issues[i].StatusHistory[j].UserFirstname
				issues[i].LastCommentDate = issues[i].StatusHistory[j].ChangeDate
			}

			switch issues[i].StatusHistory[j].PropertyKey {
			case "status_id":
				issues[i].PreviousStatus = issues[i].StatusHistory[j].OldValueName
				issues[i].PreviousStatusDate = issues[i].StatusHistory[j].ChangeDate
			case "priority_id":
				issues[i].PreviousPriority = issues[i].StatusHistory[j].OldValueName
				issues[i].PreviousPriorityDate = issues[i].StatusHistory[j].ChangeDate
			}
		}
	}

	return nil
}

// getAbsPath пытается получить абсолютный путь, игнорируя ошибку
func getAbsPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
