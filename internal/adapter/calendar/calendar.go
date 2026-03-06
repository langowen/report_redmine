package calendar

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// DayStatus представляет статус дня: 0 = рабочий, 1 = выходной
type DayStatus int

const (
	WorkDay DayStatus = 0
	DayOff  DayStatus = 1
)

// Calendar — потокобезопасный сервис для определения типа дня
type Calendar struct {
	cache map[string]map[time.Time]DayStatus // кэш по "year-month"
	mu    sync.RWMutex
}

// New создает новый экземпляр Calendar
func New() *Calendar {
	return &Calendar{
		cache: make(map[string]map[time.Time]DayStatus),
	}
}

// fetchMonth загружает календарь на весь месяц и сохраняет в кэш
func (c *Calendar) fetchMonth(year int, month time.Month) error {
	daysInMonth := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()

	url := fmt.Sprintf(
		"https://isdayoff.ru/api/getdata?year=%d&month=%d&day_from=1&day_to=%d&country=ru&pre=1&end=1",
		year, month, daysInMonth,
	)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to call API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	dayMap := make(map[time.Time]DayStatus)
	for day := 1; day <= daysInMonth; day++ {
		if day > len(body) {
			break
		}
		char := body[day-1]
		date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
		if char == '0' {
			dayMap[date] = WorkDay
		} else if char == '1' {
			dayMap[date] = DayOff
		} else {
			return fmt.Errorf("unexpected character in API response at day %d: %c", day, char)
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[c.key(year, month)] = dayMap

	return nil
}

// Key возвращает ключ для кэша (год-месяц)
func (c *Calendar) key(year int, month time.Month) string {
	return fmt.Sprintf("%d-%d", year, month)
}

// IsWorkDay возвращает true, если день рабочий
func (c *Calendar) IsWorkDay(date time.Time) (bool, error) {
	status, err := c.GetDayStatus(date)
	if err != nil {
		return false, err
	}
	return status == WorkDay, nil
}

// IsDayOff возвращает true, если день выходной или праздник
func (c *Calendar) IsDayOff(date time.Time) (bool, error) {
	status, err := c.GetDayStatus(date)
	if err != nil {
		return false, err
	}
	return status == DayOff, nil
}

// GetDayStatus возвращает статус указанного дня
func (c *Calendar) GetDayStatus(date time.Time) (DayStatus, error) {
	c.mu.RLock()
	cached, exists := c.cache[c.key(date.Year(), date.Month())]
	c.mu.RUnlock()

	if exists {
		if status, ok := cached[date]; ok {
			return status, nil
		}
	}

	// Если нет в кэше — загружаем весь месяц
	err := c.fetchMonth(date.Year(), date.Month())
	if err != nil {
		return 0, err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	status, _ := c.cache[c.key(date.Year(), date.Month())][date]
	return status, nil
}

// LoadPeriod предзагружает календарь на указанный период
func (c *Calendar) LoadPeriod(from, to time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var firstErr error

	current := time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(to.Year(), to.Month(), 1, 0, 0, 0, 0, time.UTC)

	for !current.After(end) {
		year, month, _ := current.Date()
		key := c.key(year, month)

		if _, exists := c.cache[key]; exists {
			current = current.AddDate(0, 1, 0)
			continue
		}

		daysInMonth := current.AddDate(0, 1, -1).Day()

		url := fmt.Sprintf(
			"https://isdayoff.ru/api/getdata?year=%d&month=%d&day_from=1&day_to=%d&country=ru&pre=1&end=1",
			year, month, daysInMonth,
		)

		resp, err := c.getClient().Get(url)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to load calendar for %d-%02d: %w", year, month, err)
			}
			current = current.AddDate(0, 1, 0)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to read response for %d-%02d: %w", year, month, err)
			}
			current = current.AddDate(0, 1, 0)
			continue
		}

		dayMap := make(map[time.Time]DayStatus)
		for day := 1; day <= daysInMonth; day++ {
			if day > len(body) {
				break
			}
			char := body[day-1]
			date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
			if char == '0' {
				dayMap[date] = WorkDay
			} else if char == '1' {
				dayMap[date] = DayOff
			}
		}

		c.cache[key] = dayMap
		current = current.AddDate(0, 1, 0)
	}

	return firstErr
}

// getClient возвращает HTTP-клиент с отключенной проверкой сертификата
func (c *Calendar) getClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 10 * time.Second,
	}
}
