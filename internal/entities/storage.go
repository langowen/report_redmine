package entities

import "time"

// StatusChange представляет изменение статуса задачи
type StatusChange struct {
	JournalID     int64     `json:"journal_id"`
	ChangeDate    time.Time `json:"change_date"`
	UserID        int64     `json:"user_id"`
	UserLogin     string    `json:"user_login"`
	UserFirstname string    `json:"user_firstname"`
	UserLastname  string    `json:"user_lastname"`
	PropertyKey   string    `json:"property_key"`
	OldValueID    string    `json:"old_value_id"`
	NewValueID    string    `json:"new_value_id"`
	OldValueName  string    `json:"old_value_name"`
	NewValueName  string    `json:"new_value_name"`
	Notes         string    `json:"notes,omitempty"`
}

// TimeEntry представляет трудозатраты
type TimeEntry struct {
	ID         int64     `json:"id"`
	Hours      float64   `json:"hours"`
	SpentOn    time.Time `json:"spent_on"`
	UserID     int64     `json:"user_id"`
	ActivityID int64     `json:"activity_id"`
	Comments   string    `json:"comments"`
}

// Issue представляет сырые данные задачи
type Issue struct {
	// Основная информация
	TaskNumber int    `json:"task_number"`
	Tracker    string `json:"tracker"`
	TrackerID  int64  `json:"tracker_id"`
	Theme      string `json:"theme"`

	// Статус
	CurrentStatus   string `json:"current_status"`
	CurrentStatusID int64  `json:"current_status_id"`

	// Приоритет
	Priority   string `json:"priority"`
	PriorityID int64  `json:"priority_id"`

	// Даты
	CreateDate time.Time  `json:"create_date"`
	UpdateDate time.Time  `json:"update_date"`
	ClosedDate *time.Time `json:"closed_date,omitempty"`

	// Проект
	ProjectID int64 `json:"projectId"`

	// Связанные данные
	StatusHistory []StatusChange `json:"status_history,omitempty"`
	TimeEntries   []TimeEntry    `json:"time_entries,omitempty"`

	// Вычисляемые поля (будут заполнены в Go)
	SLA              float64    `json:"sla"`
	ResolvedDate     time.Time  `json:"resolved_date"`
	StatusIntervals  []Interval `json:"status_intervals,omitempty"`
	DeadlineSLA      float64    `json:"deadline_sla"`
	MissingSLA       float64    `json:"missing_sla"`
	LastCommentator  string     `json:"last_commentator"`
	LastComment      string     `json:"last_comment"`
	ModificationDate time.Time  `json:"modification_date"`
	PreviousStatus   string     `json:"previous_status"`
	PreviousPriority string     `json:"previous_priority"`

	// Кастомные поля
	SubprojectSBS string `json:"subproject_sbs,omitempty"`
	URLJiraSBS    string `json:"url_jira_sbs,omitempty"`
	SBSTeams      string `json:"sbs_teams,omitempty"`
}

// Interval представляет временной интервал между статусами
type Interval struct {
	FromStatusID    string    `json:"from_statusId"`
	ToStatusID      string    `json:"to_status_id"`
	FromStatusName  string    `json:"from_status_name"`
	ToStatusName    string    `json:"to_status_name"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationMinutes int64     `json:"duration_minutes"`
}

// IssueRequest параметры запроса
type IssueRequest struct {
	ProjectID          int       `json:"project_id"`
	PeriodStart        time.Time `json:"period_start"`
	PeriodEnd          time.Time `json:"period_end"`
	IncludeHistory     bool      `json:"include_history"`
	IncludeTimeEntries bool      `json:"include_time_entries"`
}
