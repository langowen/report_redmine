package service

import (
	"context"
	"report_redmine/internal/entities"
)

type Storage interface {
	GetRawIssues(ctx context.Context, req entities.IssueRequest) ([]entities.Issue, error)
}
