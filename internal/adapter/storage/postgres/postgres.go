package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"report_redmine/internal/config"
	"report_redmine/internal/entities"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Storage struct {
	db  *pgxpool.Pool
	cfg *config.Config
}

func NewStorage(db *pgxpool.Pool, cfg *config.Config) *Storage {
	return &Storage{
		db:  db,
		cfg: cfg,
	}
}

func InitStorage(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	const op = "storage.postgres.New"

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s search_path=%s",
		cfg.Database.Host,
		cfg.Database.Port,
		cfg.Database.User,
		cfg.Database.Password,
		cfg.Database.DBName,
		cfg.Database.SSLMode,
		cfg.Database.Schema,
	)

	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("%s: parse config failed: %w", op, err)
	}
	poolConfig.MaxConns = 25
	poolConfig.MinConns = 5
	poolConfig.MaxConnLifetime = 10 * time.Minute
	poolConfig.MaxConnIdleTime = 5 * time.Minute

	pingCtx, cancel := context.WithTimeout(ctx, cfg.Database.Timeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(pingCtx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	if err = pool.Ping(ctx); err != nil {
		slog.Error("postgres ping failed", "error", err)
		return nil, fmt.Errorf("%s: ping failed: %w", op, err)
	}

	slog.Info("PostgreSQL storage initialized successfully")
	return pool, nil
}

// GetRawIssues возвращает сырые данные задач без расчетов
func (s *Storage) GetRawIssues(ctx context.Context, req entities.IssueRequest) ([]entities.Issue, error) {
	const op = "storage.GetRawIssues"

	// 1. Получаем основные данные задач
	issues, err := s.getBasicIssues(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}

	// 2. Если нужно, получаем историю статусов для каждой задачи
	if req.IncludeHistory {
		for i := range issues {
			statusHistory, err := s.getStatusHistory(ctx, issues[i].TaskNumber)
			if err != nil {
				slog.Warn("Failed to get status history",
					"task_id", issues[i].TaskNumber,
					"error", err)
				continue
			}
			issues[i].StatusHistory = statusHistory
		}
	}

	// 3. Если нужно, получаем трудозатраты
	if req.IncludeTimeEntries {
		for i := range issues {
			timeEntries, err := s.getTimeEntries(ctx, issues[i].TaskNumber)
			if err != nil {
				slog.Warn("Failed to get time entries",
					"task_id", issues[i].TaskNumber,
					"error", err)
				continue
			}
			issues[i].TimeEntries = timeEntries
		}
	}

	slog.Info("Raw issues fetched",
		"count", len(issues),
		"include_history", req.IncludeHistory,
		"include_time_entries", req.IncludeTimeEntries)

	return issues, nil
}

// getBasicIssues получает основную информацию о задачах
func (s *Storage) getBasicIssues(ctx context.Context, req entities.IssueRequest) ([]entities.Issue, error) {
	const op = "storage.getBasicIssues"

	query := `
		SELECT 
			i.id AS task_number,
			t.name AS tracker,
			i.subject AS theme,
			is_current.name AS current_status,
			e.name AS priority,
			i.created_on AS create_date,
			i.updated_on AS update_date,
			i.closed_on AS closed_date,
			i.project_id,
			i.tracker_id,
			i.status_id AS current_status_id,
			i.priority_id,
		
			cv39.value AS subproject_sbs,
    		cv73.value AS url_jira_sbs,
			cv110.value AS sbs_teams
			
		FROM issues i
		JOIN trackers t ON i.tracker_id = t.id
		JOIN issue_statuses is_current ON i.status_id = is_current.id
		JOIN enumerations e ON i.priority_id = e.id
		
		LEFT JOIN custom_values cv39 ON i.id = cv39.customized_id
  			  AND cv39.customized_type = 'Issue'
  			  AND cv39.custom_field_id = 39
		LEFT JOIN custom_values cv73 ON i.id = cv73.customized_id
  			  AND cv73.customized_type = 'Issue'
  			  AND cv73.custom_field_id = 73
		LEFT JOIN custom_values cv110 ON i.id = cv110.customized_id
  			  AND cv110.customized_type = 'Issue'
  			  AND cv110.custom_field_id = 110

		WHERE i.project_id = $1
		  AND (
			  (i.created_on < $2 AND i.closed_on IS NULL)
			  OR (i.created_on < $2 AND i.closed_on BETWEEN $2 AND $3)
			  OR (i.created_on BETWEEN $2 AND $3)
		  )
		ORDER BY i.id
	`

	rows, err := s.db.Query(ctx, query, req.ProjectID, req.PeriodStart, req.PeriodEnd)
	if err != nil {
		return nil, fmt.Errorf("%s: query failed: %w", op, err)
	}
	defer rows.Close()

	issues := make([]entities.Issue, 0)

	for rows.Next() {
		issue := entities.Issue{}

		err := rows.Scan(
			&issue.TaskNumber,
			&issue.Tracker,
			&issue.Theme,
			&issue.CurrentStatus,
			&issue.Priority,
			&issue.CreateDate,
			&issue.UpdateDate,
			&issue.ClosedDate,
			&issue.ProjectID,
			&issue.TrackerID,
			&issue.CurrentStatusID,
			&issue.PriorityID,
			&issue.SubprojectSBS,
			&issue.URLJiraSBS,
			&issue.SBSTeams,
		)
		if err != nil {
			return nil, fmt.Errorf("%s: scan failed: %w", op, err)
		}

		issues = append(issues, issue)
	}

	return issues, nil
}

// getStatusHistory получает историю статусов для задачи
func (s *Storage) getStatusHistory(ctx context.Context, taskID int) ([]entities.StatusChange, error) {
	const op = "storage.getStatusHistory"

	query := `
SELECT
    j.id AS journal_id,
    j.created_on AS created_date,
    j.user_id,
    jd.prop_key,
    jd.old_value,
    jd.value AS new_value,

    -- Только для статусов и приоритетов
    CASE
        WHEN jd.prop_key = 'status_id' THEN old_status.name
        WHEN jd.prop_key = 'priority_id' THEN old_priority.name
        END AS old_value_name,

    CASE
        WHEN jd.prop_key = 'status_id' THEN new_status.name
        WHEN jd.prop_key = 'priority_id' THEN new_priority.name
        END AS new_value_name,

    j.notes

FROM journals j
         JOIN journal_details jd ON j.id = jd.journal_id

         LEFT JOIN issue_statuses old_status ON jd.prop_key = 'status_id' AND jd.old_value = old_status.id::text
         LEFT JOIN issue_statuses new_status ON jd.prop_key = 'status_id' AND jd.value = new_status.id::text

         LEFT JOIN enumerations old_priority ON jd.prop_key = 'priority_id' AND jd.old_value = old_priority.id::text
         LEFT JOIN enumerations new_priority ON jd.prop_key = 'priority_id' AND jd.value = new_priority.id::text

WHERE j.journalized_id = $1
  AND j.private_notes = false
  -- Только статусы и приоритеты
  AND jd.prop_key IN ('status_id', 'priority_id')
ORDER BY j.created_on;
	`

	rows, err := s.db.Query(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("%s: query failed: %w", op, err)
	}
	defer rows.Close()

	history := make([]entities.StatusChange, 0)

	for rows.Next() {
		change := entities.StatusChange{}

		err := rows.Scan(
			&change.JournalID,
			&change.ChangeDate,
			&change.UserID,
			&change.PropertyKey,
			&change.OldValueID,
			&change.NewValueID,
			&change.OldValueName,
			&change.NewValueName,
			&change.Notes,
		)
		if err != nil {
			return nil, fmt.Errorf("%s: scan failed: %w", op, err)
		}

		history = append(history, change)
	}

	return history, nil
}

// getTimeEntries получает трудозатраты для задачи
func (s *Storage) getTimeEntries(ctx context.Context, taskID int) ([]entities.TimeEntry, error) {
	const op = "storage.getTimeEntries"

	query := `
		SELECT 
			id,
			hours,
			spent_on,
			user_id,
			activity_id,
			comments
		FROM time_entries
		WHERE issue_id = $1
		ORDER BY spent_on
	`

	rows, err := s.db.Query(ctx, query, taskID)
	if err != nil {
		return nil, fmt.Errorf("%s: query failed: %w", op, err)
	}
	defer rows.Close()

	entries := make([]entities.TimeEntry, 0)

	for rows.Next() {
		entry := entities.TimeEntry{}

		err := rows.Scan(
			&entry.ID,
			&entry.Hours,
			&entry.SpentOn,
			&entry.UserID,
			&entry.ActivityID,
			&entry.Comments,
		)
		if err != nil {
			return nil, fmt.Errorf("%s: scan failed: %w", op, err)
		}

		entries = append(entries, entry)
	}

	return entries, nil
}
