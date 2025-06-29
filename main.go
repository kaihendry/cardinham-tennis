package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/apex/gateway/v2"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

//go:embed templates
var tmpl embed.FS

//go:embed credentials.json
var credentialsData []byte

//go:embed token.json
var tokenData []byte

type CalendarConfig struct {
	GoogleCalendarID  string            `json:"google_calendar_id"`
	CredentialsFile   string            `json:"credentials_file"`
	TokenFile         string            `json:"token_file"`
	UtilizationConfig UtilizationConfig `json:"utilization_config"`
}

type UtilizationConfig struct {
	StartHour       int  `json:"start_hour"`        // Default: 6 (6 AM)
	EndHour         int  `json:"end_hour"`          // Default: 18 (6 PM)
	ShowDailyStats  bool `json:"show_daily_stats"`  // Default: true
	ShowWeeklyStats bool `json:"show_weekly_stats"` // Default: true
}

type Booking struct {
	Title     string
	StartTime time.Time
	EndTime   time.Time
	Duration  time.Duration
}

type DayStats struct {
	Date        time.Time
	Bookings    []Booking
	TotalHours  float64
	Utilization float64
}

type WeekStats struct {
	WeekStart   time.Time
	WeekEnd     time.Time
	TotalHours  float64
	Utilization float64
	Days        []DayStats
}

type PageData struct {
	Now            time.Time
	ChosenDate     time.Time
	Previous       time.Time
	Next           time.Time
	Bookings       []Booking
	DailyStats     []DayStats
	WeeklyStats    []WeekStats
	Config         UtilizationConfig
	Version        string
	CalendarID     string
	TotalBookings  int
	TotalHours     float64
	AvgUtilization float64
}

func main() {
	commit, _ := GitCommit()

	t, err := template.New("base").Funcs(template.FuncMap{
		"formatTime": func(t time.Time) string {
			if t.Hour() == 0 && t.Minute() == 0 {
				return t.Format("Mon Jan 2 (all day)")
			}
			return t.Format("Mon Jan 2 15:04")
		},
		"formatDate": func(t time.Time) string {
			return t.Format("Mon Jan 2")
		},
		"formatWeek": func(t time.Time) string {
			return t.Format("Jan 2")
		},
		"roundFloat": func(f float64) string {
			return fmt.Sprintf("%.1f", f)
		},
		"utilizationClass": func(utilization float64) string {
			if utilization >= 80.0 {
				return "utilization-high"
			} else if utilization >= 50.0 {
				return "utilization-medium"
			}
			return "utilization-low"
		},
	}).ParseFS(tmpl, "templates/*.html")

	if err != nil {
		slog.Error("Failed to parse templates", "error", err)
		return
	}

	http.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		chosenDate := time.Now()
		inputDate := r.URL.Query().Get("date")
		if inputDate != "" {
			if parsed, err := time.Parse("2006-01-02", inputDate); err == nil {
				chosenDate = parsed
			}
		}

		// Load configuration
		config := loadCalendarConfig()

		// Get calendar data with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Create a channel for the result
		resultChan := make(chan struct {
			bookings    []Booking
			dailyStats  []DayStats
			weeklyStats []WeekStats
			err         error
		}, 1)

		// Run calendar data retrieval in a goroutine
		go func() {
			bookings, dailyStats, weeklyStats, err := getCalendarData(config, chosenDate)
			resultChan <- struct {
				bookings    []Booking
				dailyStats  []DayStats
				weeklyStats []WeekStats
				err         error
			}{bookings, dailyStats, weeklyStats, err}
		}()

		// Wait for result or timeout
		var bookings []Booking
		var dailyStats []DayStats
		var weeklyStats []WeekStats
		var err error

		select {
		case result := <-resultChan:
			bookings = result.bookings
			dailyStats = result.dailyStats
			weeklyStats = result.weeklyStats
			err = result.err
		case <-ctx.Done():
			err = fmt.Errorf("calendar data retrieval timed out after 10 seconds")
		}

		if err != nil {
			slog.Error("Failed to get calendar data", "error", err)
			// Return a user-friendly error page instead of 500
			rw.Header().Set("Content-Type", "text/html")
			errorPage := fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head><title>Calendar Error</title></head>
<body style="font-family: Arial, sans-serif; max-width: 600px; margin: 50px auto; padding: 20px;">
    <h1>🎾 Cardinham Tennis Utilization</h1>
    <div style="background: #f8d7da; border: 1px solid #f5c6cb; padding: 15px; border-radius: 5px; color: #721c24;">
        <h3>Unable to load calendar data</h3>
        <p><strong>Error:</strong> %s</p>
        <p>This could be due to:</p>
        <ul>
            <li>Missing or invalid credentials.json</li>
            <li>Missing or invalid token.json</li>
            <li>Network connectivity issues</li>
            <li>Google Calendar API rate limits</li>
        </ul>
        <p>Please check the server logs for more details.</p>
    </div>
    <p><small>Generated at %s</small></p>
</body>
</html>`, err.Error(), time.Now().Format("2006-01-02 15:04:05"))
			rw.WriteHeader(http.StatusOK) // Use 200 instead of 500 for better UX
			rw.Write([]byte(errorPage))
			return
		}

		// Calculate summary statistics
		totalBookings := len(bookings)
		var totalHours float64
		var totalUtilization float64
		utilizationCount := 0

		for _, day := range dailyStats {
			totalHours += day.TotalHours
			totalUtilization += day.Utilization
			utilizationCount++
		}

		var avgUtilization float64
		if utilizationCount > 0 {
			avgUtilization = totalUtilization / float64(utilizationCount)
		}

		pageData := PageData{
			Now:            time.Now(),
			ChosenDate:     chosenDate,
			Previous:       chosenDate.AddDate(0, 0, -7),
			Next:           chosenDate.AddDate(0, 0, 7),
			Bookings:       bookings,
			DailyStats:     dailyStats,
			WeeklyStats:    weeklyStats,
			Config:         config.UtilizationConfig,
			Version:        commit,
			CalendarID:     config.GoogleCalendarID,
			TotalBookings:  totalBookings,
			TotalHours:     totalHours,
			AvgUtilization: avgUtilization,
		}

		rw.Header().Set("Content-Type", "text/html")
		err = t.ExecuteTemplate(rw, "index.html", pageData)
		if err != nil {
			slog.Error("Failed to execute templates", "error", err)
			http.Error(rw, "Internal server error", http.StatusInternalServerError)
		}
	})

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if _, ok := os.LookupEnv("AWS_LAMBDA_FUNCTION_NAME"); ok {
		err = gateway.ListenAndServe("", nil)
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
		port := os.Getenv("PORT")
		if port == "" {
			port = "8080"
		}
		err = http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	}

	slog.Error("error listening", "error", err)
}

func getCalendarData(config CalendarConfig, chosenDate time.Time) ([]Booking, []DayStats, []WeekStats, error) {
	slog.Info("Starting calendar data retrieval", "calendar_id", config.GoogleCalendarID, "start_date", chosenDate)

	// Check if credentials data is available
	if len(credentialsData) == 0 {
		return nil, nil, nil, fmt.Errorf("credentials.json not found or empty. Please ensure credentials.json is available in the project root")
	}
	slog.Info("Credentials data loaded", "size", len(credentialsData))

	// Create OAuth2 config from embedded credentials
	oauthConfig, err := google.ConfigFromJSON(credentialsData, calendar.CalendarReadonlyScope)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to parse client secret file to config: %v", err)
	}
	slog.Info("OAuth2 config created successfully")

	// Check if token data is available
	if len(tokenData) == 0 {
		return nil, nil, nil, fmt.Errorf("token.json not found or empty. Please ensure token.json is available in the project root")
	}
	slog.Info("Token data loaded", "size", len(tokenData))

	// Load token from embedded data
	var tok oauth2.Token
	if err := json.Unmarshal(tokenData, &tok); err != nil {
		return nil, nil, nil, fmt.Errorf("unable to parse token: %v", err)
	}
	slog.Info("Token parsed successfully", "expiry", tok.Expiry)

	// Create calendar service
	ctx := context.Background()
	srv, err := calendar.NewService(ctx, option.WithHTTPClient(oauthConfig.Client(ctx, &tok)))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to retrieve Calendar client: %v", err)
	}
	slog.Info("Calendar service created successfully")

	// Get events for the next 30 days from chosen date
	startTime := chosenDate
	endTime := startTime.AddDate(0, 0, 30)
	slog.Info("Fetching calendar events", "start", startTime, "end", endTime)

	events, err := srv.Events.List(config.GoogleCalendarID).
		ShowDeleted(false).
		SingleEvents(true).
		OrderBy("startTime").
		TimeMin(startTime.Format(time.RFC3339)).
		TimeMax(endTime.Format(time.RFC3339)).
		MaxResults(100).
		Do()

	if err != nil {
		return nil, nil, nil, fmt.Errorf("unable to retrieve events: %v", err)
	}
	slog.Info("Calendar events retrieved", "count", len(events.Items))

	// Parse events into bookings
	bookings := parseBookings(events.Items)
	slog.Info("Bookings parsed", "count", len(bookings))

	// Calculate statistics
	var dailyStats []DayStats
	var weeklyStats []WeekStats

	if config.UtilizationConfig.ShowDailyStats {
		dailyStats = calculateDailyStats(bookings, config.UtilizationConfig)
		slog.Info("Daily stats calculated", "count", len(dailyStats))
	}

	if config.UtilizationConfig.ShowWeeklyStats {
		weeklyStats = calculateWeeklyStats(bookings, config.UtilizationConfig)
		slog.Info("Weekly stats calculated", "count", len(weeklyStats))
	}

	slog.Info("Calendar data retrieval completed successfully")
	return bookings, dailyStats, weeklyStats, nil
}

func parseBookings(events []*calendar.Event) []Booking {
	var bookings []Booking

	for _, item := range events {
		if item.Start == nil || item.End == nil {
			continue
		}

		var startTime, endTime time.Time
		var err error

		// Parse start time
		if item.Start.DateTime != "" {
			startTime, err = time.Parse(time.RFC3339, item.Start.DateTime)
		} else {
			startTime, err = time.Parse("2006-01-02", item.Start.Date)
		}
		if err != nil {
			continue
		}

		// Parse end time
		if item.End.DateTime != "" {
			endTime, err = time.Parse(time.RFC3339, item.End.DateTime)
		} else {
			endTime, err = time.Parse("2006-01-02", item.End.Date)
		}
		if err != nil {
			continue
		}

		title := item.Summary
		if title == "" {
			title = "(No title)"
		}

		duration := endTime.Sub(startTime)

		bookings = append(bookings, Booking{
			Title:     title,
			StartTime: startTime,
			EndTime:   endTime,
			Duration:  duration,
		})
	}

	return bookings
}

func calculateDailyStats(bookings []Booking, config UtilizationConfig) []DayStats {
	// Group bookings by day
	dayMap := make(map[string][]Booking)

	for _, booking := range bookings {
		dateKey := booking.StartTime.Format("2006-01-02")
		dayMap[dateKey] = append(dayMap[dateKey], booking)
	}

	var dailyStats []DayStats

	for dateKey, dayBookings := range dayMap {
		date, _ := time.Parse("2006-01-02", dateKey)

		// Calculate total hours for the day
		var totalHours float64
		for _, booking := range dayBookings {
			// Only count hours within the facility's operating hours
			bookingHours := calculateBookingHoursInRange(booking, config.StartHour, config.EndHour)
			totalHours += bookingHours
		}

		// Calculate utilization percentage
		availableHours := float64(config.EndHour - config.StartHour)
		utilization := (totalHours / availableHours) * 100

		dailyStats = append(dailyStats, DayStats{
			Date:        date,
			Bookings:    dayBookings,
			TotalHours:  totalHours,
			Utilization: utilization,
		})
	}

	return dailyStats
}

func calculateBookingHoursInRange(booking Booking, startHour, endHour int) float64 {
	// If it's an all-day event, count full operating hours
	if booking.StartTime.Hour() == 0 && booking.StartTime.Minute() == 0 {
		return float64(endHour - startHour)
	}

	// Calculate the effective start and end times within operating hours
	effectiveStart := booking.StartTime
	effectiveEnd := booking.EndTime

	// Adjust start time if it's before operating hours
	if effectiveStart.Hour() < startHour {
		effectiveStart = time.Date(effectiveStart.Year(), effectiveStart.Month(), effectiveStart.Day(), startHour, 0, 0, 0, effectiveStart.Location())
	}

	// Adjust end time if it's after operating hours
	if effectiveEnd.Hour() > endHour {
		effectiveEnd = time.Date(effectiveEnd.Year(), effectiveEnd.Month(), effectiveEnd.Day(), endHour, 0, 0, 0, effectiveEnd.Location())
	}

	// Calculate duration in hours
	duration := effectiveEnd.Sub(effectiveStart)
	hours := duration.Hours()

	// Ensure we don't return negative hours
	if hours < 0 {
		return 0
	}

	return hours
}

func calculateWeeklyStats(bookings []Booking, config UtilizationConfig) []WeekStats {
	// Group bookings by week
	weekMap := make(map[string][]Booking)

	for _, booking := range bookings {
		// Get the start of the week (Monday)
		weekStart := getWeekStart(booking.StartTime)
		weekKey := weekStart.Format("2006-01-02")
		weekMap[weekKey] = append(weekMap[weekKey], booking)
	}

	var weeklyStats []WeekStats

	for weekKey, weekBookings := range weekMap {
		weekStart, _ := time.Parse("2006-01-02", weekKey)
		weekEnd := weekStart.AddDate(0, 0, 6)

		// Calculate daily stats for this week
		dailyStats := calculateDailyStats(weekBookings, config)

		// Calculate total hours for the week
		var totalHours float64
		for _, day := range dailyStats {
			totalHours += day.TotalHours
		}

		// Calculate utilization percentage (7 days * hours per day)
		availableHours := float64(7 * (config.EndHour - config.StartHour))
		utilization := (totalHours / availableHours) * 100

		weeklyStats = append(weeklyStats, WeekStats{
			WeekStart:   weekStart,
			WeekEnd:     weekEnd,
			TotalHours:  totalHours,
			Utilization: utilization,
			Days:        dailyStats,
		})
	}

	return weeklyStats
}

func getWeekStart(date time.Time) time.Time {
	// Get Monday of the week
	weekday := date.Weekday()
	daysToSubtract := int(weekday - time.Monday)
	if daysToSubtract < 0 {
		daysToSubtract += 7
	}
	return date.AddDate(0, 0, -daysToSubtract)
}

func loadCalendarConfig() CalendarConfig {
	// Try to load from config.json first (look in parent directory)
	if _, err := os.Stat("../config.json"); err == nil {
		data, err := os.ReadFile("../config.json")
		if err == nil {
			var config CalendarConfig
			if json.Unmarshal(data, &config) == nil {
				// Set defaults for utilization config if not provided
				if config.UtilizationConfig.StartHour == 0 {
					config.UtilizationConfig.StartHour = 6
				}
				if config.UtilizationConfig.EndHour == 0 {
					config.UtilizationConfig.EndHour = 18
				}
				if !config.UtilizationConfig.ShowDailyStats && !config.UtilizationConfig.ShowWeeklyStats {
					config.UtilizationConfig.ShowDailyStats = true
					config.UtilizationConfig.ShowWeeklyStats = true
				}
				return config
			}
		}
	}

	// Fallback to environment variables or defaults
	calendarID := os.Getenv("GOOGLE_CALENDAR_ID")
	if calendarID == "" {
		calendarID = "cardinhamsports@gmail.com"
	}

	credentialsFile := os.Getenv("GOOGLE_CREDENTIALS_FILE")
	if credentialsFile == "" {
		credentialsFile = "credentials.json"
	}

	tokenFile := os.Getenv("GOOGLE_TOKEN_FILE")
	if tokenFile == "" {
		tokenFile = "token.json"
	}

	// Parse utilization config from environment
	startHour := 6 // Default 6 AM
	if startStr := os.Getenv("UTILIZATION_START_HOUR"); startStr != "" {
		if start, err := fmt.Sscanf(startStr, "%d", &startHour); err != nil || start != 1 {
			startHour = 6
		}
	}

	endHour := 18 // Default 6 PM
	if endStr := os.Getenv("UTILIZATION_END_HOUR"); endStr != "" {
		if end, err := fmt.Sscanf(endStr, "%d", &endHour); err != nil || end != 1 {
			endHour = 18
		}
	}

	return CalendarConfig{
		GoogleCalendarID: calendarID,
		CredentialsFile:  credentialsFile,
		TokenFile:        tokenFile,
		UtilizationConfig: UtilizationConfig{
			StartHour:       startHour,
			EndHour:         endHour,
			ShowDailyStats:  true,
			ShowWeeklyStats: true,
		},
	}
}

func GitCommit() (commit string, dirty bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.modified":
			dirty = setting.Value == "true"
		case "vcs.revision":
			commit = setting.Value
		}
	}
	return
}
